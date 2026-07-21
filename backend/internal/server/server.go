package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"hv-launcher/internal/auth"
	"hv-launcher/internal/config"
	"hv-launcher/internal/cpuidmodule"
	"hv-launcher/internal/hypervisor"
	"hv-launcher/internal/jobs"
	"hv-launcher/internal/manage"
	"hv-launcher/internal/proton"
	"hv-launcher/internal/system"
	"hv-launcher/internal/umip"
)

const (
	maxRequestBytes = 64 << 10
	deckyOrigin     = "https://steamloopback.host"
)

type Options struct {
	ListenAddress   string
	Config          *config.Store
	Inspector       *system.Inspector
	Manager         *manage.Manager
	Controller      *hypervisor.Controller
	ProcessReader   system.Reader
	ProcRoot        string
	Logger          *slog.Logger
	Proton          proton.Operator
	Jobs            *jobs.Coordinator
	UMIP            *umip.Inspector
	Capabilities    *auth.Verifier
	ModuleInspector *cpuidmodule.Inspector
	ModulePreflight *cpuidmodule.PreflightInspector
}

type Service struct {
	options   Options
	server    *http.Server
	accepting atomic.Bool
	limiter   *transitionLimiter
}

func New(options Options) (*Service, error) {
	if options.Config == nil || options.Inspector == nil || options.Manager == nil || options.Controller == nil ||
		options.Proton == nil || options.Jobs == nil || options.UMIP == nil ||
		options.Capabilities == nil || options.ModuleInspector == nil || options.ModulePreflight == nil {
		return nil, errors.New("configuration, inspector, manager, controller, and setup services are required")
	}
	if err := validateLoopbackAddress(options.ListenAddress); err != nil {
		return nil, err
	}
	if options.ProcessReader == nil {
		options.ProcessReader = system.OSReader{}
	}
	if options.ProcRoot == "" {
		options.ProcRoot = "/proc"
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	service := &Service{options: options, limiter: newTransitionLimiter(30, time.Second)}
	service.accepting.Store(true)
	service.server = &http.Server{
		Addr:              options.ListenAddress,
		Handler:           service.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return service, nil
}

func (s *Service) routes() http.Handler {
	router := chi.NewRouter()
	router.Use(middleware.Recoverer)
	router.Use(deckyCORS)
	router.Route("/v1", func(api chi.Router) {
		api.Get("/status", s.status)
		api.Get("/config", s.configuration)
		api.Post("/games/{appID}/enable", s.enableGame)
		api.Post("/games/{appID}/disable", s.disableGame)
		api.Post("/lifetime", s.lifetime)
		api.Post("/sessions", s.startSession)
		api.Delete("/sessions/{sessionID}", s.endSession)
		api.Post("/setup/proton/preflight", s.preflightProtonArchive)
		api.Post("/setup/proton/install", s.installProtonArchive)
		api.Get("/setup/umip", s.inspectUMIP)
		api.Post("/setup/umip", s.applyUMIP)
		api.Get("/setup/module/preflight", s.inspectModuleRequirements)
		api.Get("/setup/jobs/active", s.activeSetupJob)
		api.Get("/setup/jobs/{jobID}", s.setupJob)
		api.Get("/setup/events", s.setupEvents)
	})
	return router
}

func deckyCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && origin != deckyOrigin {
			writeError(w, http.StatusForbidden, errors.New("origin is not allowed"))
			return
		}
		if origin == deckyOrigin {
			w.Header().Set("Access-Control-Allow-Origin", deckyOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) Handler() http.Handler { return s.server.Handler }

func (s *Service) Serve(ctx context.Context) error {
	listener, err := net.Listen("tcp4", s.options.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.options.ListenAddress, err)
	}
	s.options.Logger.Info("HTTP service listening", "address", s.options.ListenAddress)
	serveErr := make(chan error, 1)
	go func() { serveErr <- s.server.Serve(listener) }()
	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		s.options.Logger.Info("HTTP service shutting down")
		s.accepting.Store(false)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownErr := s.server.Shutdown(shutdownCtx)
		restoreErr := s.options.Controller.Shutdown(shutdownCtx)
		result := errors.Join(shutdownErr, restoreErr)
		if result != nil {
			s.options.Logger.Error("backend shutdown failed", "error", result)
		} else {
			s.options.Logger.Info("backend shutdown complete")
		}
		return result
	}
}

func validateLoopbackAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" || port == "" {
		return fmt.Errorf("service must bind to an explicit 127.0.0.1 port")
	}
	return nil
}
