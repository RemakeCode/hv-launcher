package server

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"hv-launcher/internal/auth"
	"hv-launcher/internal/jobs"
)

type moduleInstallRequest struct {
	Path       string `json:"path"`
	Capability string `json:"capability"`
}

func (s *Service) installModule(w http.ResponseWriter, r *http.Request) {
	var request moduleInstallRequest
	if !decodeStrict(w, r, &request) {
		return
	}
	if request.Path == "" || len(request.Path) > maxSetupPathBytes || !filepath.IsAbs(request.Path) || strings.ContainsRune(request.Path, '\x00') {
		writeError(w, http.StatusBadRequest, errors.New("selected module archive path must be an absolute bounded path"))
		return
	}
	preflight := s.options.ModulePreflight.Inspect(string(s.options.Controller.State()))
	if preflight.ControllerState != "idle" {
		writeError(w, http.StatusConflict, errors.New("the hypervisor manager must be idle before module setup"))
		return
	}
	if !preflight.Ready && preflight.DependencyPlan == nil {
		writeError(w, http.StatusConflict, errors.New("module requirements are not ready and no supported dependency plan is available"))
		return
	}

	if err := s.options.Capabilities.Consume(request.Capability, auth.OperationModuleInstall, request.Path); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}

	started, err := s.options.Jobs.Start("module-install", "starting", func(job *jobs.Job) (any, error) {
		result, installErr := s.options.ModuleInstaller.Install(context.Background(), request.Path, preflight.DependencyPlan, func(phase string, progress int, output string) {
			job.Update(phase, progress)
			job.Output(output)
		})

		if installErr != nil {
			s.options.Logger.Error("CPUID module installation failed", "path", request.Path, "error", installErr)
			return nil, installErr
		}

		s.options.Logger.Info("CPUID module installation complete", "module", result.ModuleName, "kernel", result.KernelRelease, "no_op", result.NoOp, "signing_required", result.SigningRequired)
		return result, nil
	})

	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, jobs.ErrBusy) {
			status = http.StatusConflict
		}
		writeError(w, status, err)
		return
	}

	writeJSON(w, http.StatusAccepted, started)
}
