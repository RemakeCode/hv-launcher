package server

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"time"

	"hv-launcher/internal/jobs"
	"hv-launcher/internal/proton"
)

const maxSetupPathBytes = 4_096

type protonPreflightRequest struct {
	Path string `json:"path"`
}

type protonPreflightResponse struct {
	SelectionID    string           `json:"selectionId"`
	ExpiresAt      time.Time        `json:"expiresAt"`
	Preflight      proton.Preflight `json:"preflight"`
	Responsibility string           `json:"responsibility"`
}

type protonInstallRequest struct {
	SelectionID     string `json:"selectionId"`
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
	record, err := s.options.ProtonSelections.Put(request.Path, preflight)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}
	s.options.Logger.Info("Proton archive preflight complete", "path", request.Path, "selection_id", record.ID)
	writeJSON(w, http.StatusOK, protonPreflightResponse{
		SelectionID:    record.ID,
		ExpiresAt:      record.ExpiresAt,
		Preflight:      preflight,
		Responsibility: "HV Launcher cannot verify this archive's publisher, authenticity, or suitability. Confirm that you sourced and selected the intended archive before installing.",
	})
}

func (s *Service) installProtonArchive(w http.ResponseWriter, r *http.Request) {
	var request protonInstallRequest
	if !decodeStrict(w, r, &request) {
		return
	}
	if request.SelectionID == "" || len(request.SelectionID) > 128 || request.DestinationID == "" || len(request.DestinationID) > 128 {
		writeError(w, http.StatusBadRequest, errors.New("selection ID and destination ID are required"))
		return
	}
	if !request.ConfirmedSource {
		writeError(w, http.StatusBadRequest, errors.New("confirm that you sourced and selected the intended Proton archive"))
		return
	}
	record, err := s.options.ProtonSelections.Get(request.SelectionID)
	if err != nil {
		writeError(w, http.StatusGone, err)
		return
	}
	if !preflightContainsDestination(record.Preflight, request.DestinationID) {
		writeError(w, http.StatusBadRequest, errors.New("destination was not offered for this selection"))
		return
	}
	started, err := s.options.Jobs.Start("proton-install", "starting", func(job *jobs.Job) (any, error) {
		result, installErr := s.options.Proton.Install(context.Background(), record.Path, request.DestinationID, func(phase string, progress int, message string) {
			job.Update(phase, progress)
			job.Output(message)
		})
		if installErr != nil {
			s.options.Logger.Error("Proton installation failed", "selection_id", record.ID, "error", installErr)
			return nil, installErr
		}
		s.options.ProtonSelections.Delete(record.ID)
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

func preflightContainsDestination(preflight proton.Preflight, requestedID string) bool {
	for _, destination := range preflight.Destinations {
		if destination.ID == requestedID {
			return true
		}
	}
	return false
}
