package server

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"

	"hv-launcher/internal/jobs"
	"hv-launcher/internal/proton"
)

const maxSetupPathBytes = 4_096

type protonPreflightRequest struct {
	Path string `json:"path"`
}

type protonPreflightResponse struct {
	Preflight      proton.Preflight `json:"preflight"`
	Responsibility string           `json:"responsibility"`
}

type protonInstallRequest struct {
	Path            string `json:"path"`
	DestinationID   string `json:"destinationId"`
	ConfirmedSource bool   `json:"confirmedSource"`
}

func (s *Service) preflightProtonArchive(w http.ResponseWriter, r *http.Request) {
	var request protonPreflightRequest
	if !decodeStrict(w, r, &request) {
		return
	}
	if request.Path == "" || len(request.Path) > maxSetupPathBytes || !filepath.IsAbs(request.Path) {
		writeError(w, http.StatusBadRequest, errors.New("selected archive path must be an absolute bounded path"))
		return
	}
	preflight, err := s.options.Proton.PreflightPath(request.Path)
	if err != nil {
		s.options.Logger.Warn("Proton archive preflight failed", "path", request.Path, "error", err)
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	s.options.Logger.Info("Proton archive preflight complete", "path", request.Path)
	writeJSON(w, http.StatusOK, protonPreflightResponse{
		Preflight:      preflight,
		Responsibility: "HV Launcher cannot verify this archive's publisher, authenticity, or suitability. Confirm that you sourced and selected the intended archive before installing.",
	})
}

func (s *Service) installProtonArchive(w http.ResponseWriter, r *http.Request) {
	var request protonInstallRequest
	if !decodeStrict(w, r, &request) {
		return
	}
	if request.Path == "" || len(request.Path) > maxSetupPathBytes || !filepath.IsAbs(request.Path) ||
		request.DestinationID == "" || len(request.DestinationID) > 128 {
		writeError(w, http.StatusBadRequest, errors.New("an absolute bounded archive path and destination ID are required"))
		return
	}
	if !request.ConfirmedSource {
		writeError(w, http.StatusBadRequest, errors.New("confirm that you sourced and selected the intended Proton archive"))
		return
	}
	started, err := s.options.Jobs.Start("proton-install", "starting", func(job *jobs.Job) (any, error) {
		result, installErr := s.options.Proton.Install(context.Background(), request.Path, request.DestinationID, func(phase string, progress int, message string) {
			job.Update(phase, progress)
			job.Output(message)
		})
		if installErr != nil {
			s.options.Logger.Error("Proton installation failed", "path", request.Path, "error", installErr)
			return nil, installErr
		}
		job.Output("Compatibility tool installed. Steam must be restarted before selecting it.")
		s.options.Logger.Info("Proton installation complete", "tool", result.ToolName, "destination_id", result.DestinationID)
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
