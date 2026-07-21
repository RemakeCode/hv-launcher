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

	"hv-launcher/internal/auth"
	"hv-launcher/internal/config"
	"hv-launcher/internal/cpuidmodule"
	"hv-launcher/internal/hypervisor"
	"hv-launcher/internal/jobs"
	backendlogger "hv-launcher/internal/logger"
	"hv-launcher/internal/proton"
	"hv-launcher/internal/server"
	"hv-launcher/internal/shortcuts"
	"hv-launcher/internal/steamprocess"
	"hv-launcher/internal/system"
	"hv-launcher/internal/umip"
	"hv-launcher/internal/wrapper"
)

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
		return serveBackend()
	}
	switch args[0] {
	case "run":
		return runWrapped(args[1:])
	case proton.WorkerCommand:
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		return proton.RunWorker(ctx, os.Stdin, os.Stdout)
	default:
		return fmt.Errorf("unknown command %q (expected run, %s, or no command)", args[0], proton.WorkerCommand)
	}
}

func serveBackend() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	capabilities, err := auth.LoadEnvironment()
	if err != nil {
		return fmt.Errorf("configure privileged setup authorization: %w", err)
	}

	userHome := os.Getenv("DECKY_USER_HOME")
	if userHome == "" {
		return errors.New("DECKY_USER_HOME is required")
	}

	journal, err := hypervisor.NewDeckyFileJournal()
	if err != nil {
		return fmt.Errorf("configure transition journal: %w", err)
	}

	userInfo, err := os.Stat(userHome)
	if err != nil {
		return fmt.Errorf("inspect Decky user home: %w", err)
	}
	userStat, ok := userInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("Decky user ownership is unavailable")
	}

	logger := slog.Default()
	reader := system.OSReader{}
	logger.Info("backend starting", "listen_address", server.ListenAddress, "effective_uid", os.Geteuid())

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

	protonWorker, err := proton.NewWorkerClient(executable, userHome, int(userStat.Uid), int(userStat.Gid))
	if err != nil {
		return fmt.Errorf("configure unprivileged Proton worker: %w", err)
	}

	kernelData, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return fmt.Errorf("read kernel release: %w", err)
	}

	moduleState := hypervisor.SysModuleState{Reader: reader, Root: "/sys/module"}
	controller, err := hypervisor.New(hypervisor.Options{
		Runner:        hypervisor.ExecRunner{},
		Modules:       moduleState,
		Journal:       journal,
		KernelRelease: string(bytes.TrimSpace(kernelData)),
		Logger:        logger,
	})
	if err != nil {
		return err
	}

	if err := controller.Reconcile(ctx, runningManagedShortcuts(store, reader)); err != nil && !errors.Is(err, hypervisor.ErrRecoveryRequired) {
		return fmt.Errorf("reconcile prior transition: %w", err)
	}

	svc, err := server.New(server.Options{
		Config:          store,
		Inspector:       system.NewInspector(userHome),
		Manager:         &shortcuts.Manager{Store: store, WrapperPath: executable},
		Controller:      controller,
		Logger:          logger,
		Proton:          protonWorker,
		Jobs:            jobs.NewCoordinator(),
		UMIP:            umip.NewInspector(umip.DefaultPaths()),
		Capabilities:    capabilities,
		ModuleInspector: cpuidmodule.NewInspector(),
		ModulePreflight: cpuidmodule.NewPreflightInspector(cpuidmodule.DefaultPreflightPaths()),
		ModuleInstaller: cpuidmodule.NewInstaller(cpuidmodule.DefaultPreflightPaths(), cpuidmodule.ExecPackageCommandRunner{}),
	})
	if err != nil {
		return err
	}

	logger.Info("backend ready", "controller_state", controller.State(), "managed_shortcuts", len(store.Snapshot().Games))
	return svc.Serve(ctx)
}

func runningManagedShortcuts(store *config.Store, reader system.Reader) map[string]bool {
	games := store.Snapshot().Games
	enabled := make(map[string]bool, len(games))
	for id := range games {
		enabled[id] = true
	}

	running := make(map[string]bool)
	for _, id := range steamprocess.ResolveRunningShortcutIDs(reader, "/proc", enabled) {
		running[id] = true
	}
	return running
}

func runWrapped(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	appID := flags.String("app-id", "", "managed Steam App ID")
	baseURL := flags.String("service-url", "http://"+server.ListenAddress+"/v1", "Go service URL")
	if err := flags.Parse(args); err != nil {
		return err
	}

	command := flags.Args()
	if *appID == "" {
		return errors.New("--app-id is required")
	}
	if len(command) == 0 {
		return errors.New("original command is required after '--'")
	}

	ctx := context.Background()
	return wrapper.Run(ctx, wrapper.Options{
		AppID:       *appID,
		BaseURL:     *baseURL,
		Command:     command,
		HTTPTimeout: 30 * time.Second,
	})
}
