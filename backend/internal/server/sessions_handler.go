package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"hv-launcher/internal/hypervisor"
	"hv-launcher/internal/model"
	"hv-launcher/internal/steam"
)

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
