package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"hv-launcher/internal/config"
	"hv-launcher/internal/hypervisor"
	backendlogger "hv-launcher/internal/logger"
	"hv-launcher/internal/manage"
	"hv-launcher/internal/server"
	"hv-launcher/internal/steam"
	"hv-launcher/internal/system"
	"hv-launcher/internal/wrapper"
)

const defaultListenAddress = "127.0.0.1:42991"

func main() {
	logger := backendlogger.Configure(os.Stdout)
	if err := run(os.Args[1:]); err != nil {
		logger.Error("hv-launcher failed", "error", err)
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			os.Exit(exitError.ExitCode())
		}
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return serve()
	}
	switch args[0] {
	case "run":
		return runWrapped(args[1:])
	default:
		return fmt.Errorf("unknown command %q (expected run or no command)", args[0])
	}
}

func serve() error {
	runtimeDir := os.Getenv("DECKY_PLUGIN_RUNTIME_DIR")
	userHome := os.Getenv("DECKY_USER_HOME")
	if runtimeDir == "" || userHome == "" {
		return errors.New("DECKY_PLUGIN_RUNTIME_DIR and DECKY_USER_HOME are required")
	}
	logger := slog.Default()
	logger.Info("backend starting", "listen_address", defaultListenAddress, "effective_uid", os.Geteuid())
	dataDir, err := config.DataDir(userHome, os.Getenv("XDG_DATA_HOME"))
	if err != nil {
		return fmt.Errorf("resolve configuration directory: %w", err)
	}
	store, err := config.Open(dataDir)
	if err != nil {
		return fmt.Errorf("open configuration: %w", err)
	}
	logger.Info("configuration ready", "directory", dataDir)

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	wrapperPath, err := wrapper.Install(executable, userHome)
	if err != nil {
		return fmt.Errorf("install persistent wrapper: %w", err)
	}
	kernelData, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return fmt.Errorf("read kernel release: %w", err)
	}
	moduleState := hypervisor.SysModuleState{Reader: system.OSReader{}, Root: "/sys/module"}
	controller, err := hypervisor.New(hypervisor.Options{
		Runner: hypervisor.ExecRunner{}, Modules: moduleState,
		Journal: hypervisor.NewFileJournal(runtimeDir), KernelRelease: string(bytes.TrimSpace(kernelData)), Logger: logger,
	})
	if err != nil {
		return err
	}
	enabled := map[string]bool{}
	for id := range store.Snapshot().Games {
		enabled[id] = true
	}
	runningIDs := steam.ResolveRunningShortcutIDs(system.OSReader{}, "/proc", enabled)
	running := map[string]bool{}
	for _, id := range runningIDs {
		running[id] = true
	}
	if err := controller.Reconcile(context.Background(), running); err != nil && !errors.Is(err, hypervisor.ErrRecoveryRequired) {
		return fmt.Errorf("reconcile prior transition: %w", err)
	}
	svc, err := server.New(server.Options{
		ListenAddress: defaultListenAddress, Config: store,
		Inspector: system.NewInspector(userHome, runtimeDir),
		Manager:   &manage.Manager{Store: store, WrapperPath: wrapperPath}, Controller: controller,
		Logger: logger,
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	logger.Info("backend ready", "controller_state", controller.State(), "managed_shortcuts", len(store.Snapshot().Games))
	return svc.Serve(ctx)
}

func runWrapped(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	appID := flags.String("app-id", "", "managed Steam App ID")
	baseURL := flags.String("service-url", "http://"+defaultListenAddress+"/v1", "Go service URL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	command := flags.Args()
	if *appID == "" {
		return errors.New("--app-id is required")
	}
	if len(command) == 0 {
		return errors.New("original command is required after --")
	}
	ctx := context.Background()
	return wrapper.Run(ctx, wrapper.Options{
		AppID:       *appID,
		BaseURL:     *baseURL,
		Command:     command,
		HTTPTimeout: 30 * time.Second,
	})
}
