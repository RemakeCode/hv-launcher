package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"hv-launcher/internal/model"
)

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

	var request struct{}
	if !decodeStrict(w, r, &request) {
		return
	}

	if err := s.options.Manager.Disable(appID); err != nil {
		s.options.Logger.Error("failed to disable shortcut management", "app_id", appID, "error", err)
		writeError(w, http.StatusNotFound, err)
		return
	}

	s.options.Logger.Info("shortcut management disabled", "app_id", appID)
	w.WriteHeader(http.StatusNoContent)
}
