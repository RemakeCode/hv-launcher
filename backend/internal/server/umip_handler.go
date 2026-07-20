package server

import (
	"context"
	"errors"
	"net/http"

	"hv-launcher/internal/auth"
	"hv-launcher/internal/jobs"
	"hv-launcher/internal/umip"
)

type umipApplyRequest struct {
	Bootloader umip.Bootloader `json:"bootloader"`
	Capability string          `json:"capability"`
}

func (s *Service) inspectUMIP(w http.ResponseWriter, r *http.Request) {
	status, err := s.options.Inspector.Inspect(r.Context(), string(s.options.Controller.State()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.options.UMIP.Inspect(status.CPU.UMIPPresent))
}

func (s *Service) applyUMIP(w http.ResponseWriter, r *http.Request) {
	var request umipApplyRequest
	if !decodeStrict(w, r, &request) {
		return
	}
	if request.Bootloader != umip.BootloaderLimine && request.Bootloader != umip.BootloaderGRUB {
		writeError(w, http.StatusBadRequest, umip.ErrUnsupportedBootloader)
		return
	}
	binding := umip.UMIPApplyBinding(request.Bootloader)
	if err := s.options.Capabilities.Consume(request.Capability, auth.OperationUMIPApply, binding); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	started, err := s.options.Jobs.Start("umip-apply", "starting", func(job *jobs.Job) (any, error) {
		result, applyErr := s.options.UMIP.Apply(context.Background(), request.Bootloader, func(phase string, progress int, message string) {
			job.Update(phase, progress)
			job.Output(message)
		})
		if applyErr != nil {
			s.options.Logger.Error("UMIP configuration failed", "bootloader", request.Bootloader, "error", applyErr)
			return nil, applyErr
		}
		s.options.Logger.Info("UMIP configuration complete", "bootloader", request.Bootloader, "restart_required", result.RestartRequired)
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
