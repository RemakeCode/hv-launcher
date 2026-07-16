package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"hv-launcher/internal/config"
	"hv-launcher/internal/hypervisor"
	"hv-launcher/internal/manage"
	"hv-launcher/internal/model"
	"hv-launcher/internal/steam"
	"hv-launcher/internal/system"
)

const (
	maxRequestBytes = 64 << 10
	deckyOrigin     = "https://steamloopback.host"
)

type Options struct {
	ListenAddress string
	Config        *config.Store
	Inspector     *system.Inspector
	Manager       *manage.Manager
	Controller    *hypervisor.Controller
	ProcessReader system.Reader
	ProcRoot      string
	Logger        *slog.Logger
}

type Service struct {
	options   Options
	server    *http.Server
	accepting atomic.Bool
	limiter   *transitionLimiter
}

func New(options Options) (*Service, error) {
	if options.Config == nil || options.Inspector == nil || options.Manager == nil || options.Controller == nil {
		return nil, errors.New("configuration, inspector, manager, and controller are required")
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

func (s *Service) status(w http.ResponseWriter, r *http.Request) {
	status, err := s.options.Inspector.Inspect(r.Context(), string(s.options.Controller.State()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Service) configuration(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.options.Config.Snapshot())
}

func (s *Service) enableGame(w http.ResponseWriter, r *http.Request) {
	appID, ok := validAppID(chi.URLParam(r, "appID"))
	if !ok {
		writeError(w, http.StatusBadRequest, errors.New("invalid App ID"))
		return
	}
	var request model.ManageGameRequest
	if !decodeStrict(w, r, &request) {
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" || len(request.Name) > 256 {
		writeError(w, http.StatusBadRequest, errors.New("game name must be between 1 and 256 characters"))
		return
	}
	status, err := s.options.Inspector.Inspect(r.Context(), string(s.options.Controller.State()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if status.Status != model.StatusHypervisorReady {
		writeError(w, http.StatusConflict, errors.New("per-game management is available only on a hypervisor-ready host"))
		return
	}
	managed, err := s.options.Manager.Enable(appID, request.Name, request.Shortcut, request.CurrentLaunch)
	if err != nil {
		s.options.Logger.Error("failed to enable shortcut management", "app_id", appID, "name", request.Name, "error", err)
		writeError(w, http.StatusConflict, err)
		return
	}
	s.options.Logger.Info("shortcut management enabled", "app_id", appID, "name", request.Name)
	writeJSON(w, http.StatusOK, model.ManageGameResponse{AppID: appID, ManagedLaunch: managed.ManagedLaunch, WrapperPath: managed.WrapperPath})
}

func (s *Service) disableGame(w http.ResponseWriter, r *http.Request) {
	appID, ok := validAppID(chi.URLParam(r, "appID"))
	if !ok {
		writeError(w, http.StatusBadRequest, errors.New("invalid App ID"))
		return
	}
	var request model.RestoreRequest
	if !decodeStrict(w, r, &request) {
		return
	}
	response, err := s.options.Manager.Restore(appID, request.CurrentLaunch)
	if err != nil {
		s.options.Logger.Error("failed to restore shortcut launch options", "app_id", appID, "error", err)
		writeError(w, http.StatusNotFound, err)
		return
	}
	s.options.Logger.Info("shortcut management disabled", "app_id", appID, "conflict", response.Conflict)
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) startSession(w http.ResponseWriter, r *http.Request) {
	if !s.accepting.Load() {
		writeError(w, http.StatusServiceUnavailable, errors.New("service is shutting down"))
		return
	}
	if !s.limiter.Allow() {
		writeError(w, http.StatusTooManyRequests, errors.New("session transition rate exceeded"))
		return
	}
	var request model.SessionStartRequest
	if !decodeStrict(w, r, &request) {
		return
	}
	appID, ok := validAppID(request.AppID)
	if !ok {
		writeError(w, http.StatusBadRequest, errors.New("invalid App ID"))
		return
	}
	if _, enabled := s.options.Config.Game(appID); !enabled {
		writeError(w, http.StatusForbidden, errors.New("App ID is not enabled"))
		return
	}
	session, err := s.options.Controller.StartSession(r.Context(), appID, "wrapper")
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, hypervisor.ErrKVMBusy) {
			status = http.StatusLocked
		}
		s.options.Logger.Error("session start request failed", "app_id", appID, "status", status, "error", err)
		writeError(w, status, err)
		return
	}
	writeJSON(w, http.StatusOK, model.SessionStartResponse{SessionID: session.ID})
}

func (s *Service) endSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionID")
	if err := s.options.Controller.EndSession(r.Context(), sessionID); err != nil {
		s.options.Logger.Error("session end request failed", "session_id", sessionID, "error", err)
		writeError(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) lifetime(w http.ResponseWriter, r *http.Request) {
	var request model.LifetimeRequest
	if !decodeStrict(w, r, &request) {
		return
	}
	if request.AppID == "0" {
		ids := steam.ResolveRunningShortcutIDs(s.options.ProcessReader, s.options.ProcRoot, s.enabledIDs())
		if len(ids) != 1 {
			writeJSON(w, http.StatusAccepted, map[string]string{"status": "unresolved"})
			return
		}
		request.AppID = ids[0]
	}
	appID, ok := validAppID(request.AppID)
	if !ok {
		writeError(w, http.StatusBadRequest, errors.New("invalid App ID"))
		return
	}
	request.AppID = appID
	if err := s.options.Controller.ObserveLifetime(r.Context(), request); err != nil {
		s.options.Logger.Error("Steam lifetime handling failed", "app_id", request.AppID, "instance_id", request.InstanceID, "running", request.Running, "error", err)
		writeError(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) enabledIDs() map[string]bool {
	result := map[string]bool{}
	for id := range s.options.Config.Snapshot().Games {
		result[id] = true
	}
	return result
}

func decodeStrict(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, errors.New("request must contain one JSON object"))
		return false
	}
	return true
}

func validAppID(value string) (string, bool) {
	if value == "" || len(value) > 10 {
		return "", false
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil || parsed == 0 {
		return "", false
	}
	return strconv.FormatUint(parsed, 10), true
}

func validateLoopbackAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" || port == "" {
		return fmt.Errorf("service must bind to an explicit 127.0.0.1 port")
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type transitionLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	started time.Time
	count   int
}

func newTransitionLimiter(limit int, window time.Duration) *transitionLimiter {
	return &transitionLimiter{limit: limit, window: window, started: time.Now()}
}

func (l *transitionLimiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.started) >= l.window {
		l.started, l.count = now, 0
	}
	if l.count >= l.limit {
		return false
	}
	l.count++
	return true
}
