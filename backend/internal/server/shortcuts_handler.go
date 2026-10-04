package server

import (
	"errors"
	"net/http"
	"strings"

	"hv-launcher/internal/linuwux"
	"hv-launcher/internal/model"

	"github.com/go-chi/chi/v5"
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
	mode, err := s.resolveMode(r, request.Mode)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	managed, err := s.options.Manager.EnableWithMode(appID, request.Name, request.Shortcut, request.CurrentLaunch, mode)
	if err != nil {
		s.options.Logger.Error("failed to enable shortcut management", "app_id", appID, "name", request.Name, "error", err)
		writeError(w, http.StatusConflict, err)
		return
	}
	s.options.Logger.Info("shortcut management enabled", "app_id", appID, "name", request.Name)
	writeJSON(w, http.StatusOK, manageResponse(managed))
}

func (s *Service) resolveMode(r *http.Request, requested linuwux.Mode) (linuwux.Mode, error) {
	requested = requested.ResolveLegacyMode()
	if !requested.Valid() {
		return "", errors.New("mode must be proton or runtime")
	}
	status, err := s.options.Inspector.Inspect(r.Context(), string(s.options.Controller.State()))
	if err != nil {
		return "", err
	}
	if requested == linuwux.ModeProton && !status.LinUwUx.Proton.Found {
		return "", errors.New("LinUwUx Proton is not currently available")
	}
	if requested == linuwux.ModeRuntime && !status.LinUwUx.Runtime.Available {
		return "", errors.New("LinUwUx runtime is not currently available")
	}
	return requested, nil
}

func manageResponse(game model.ManagedGame) model.ManageGameResponse {
	return model.ManageGameResponse{
		AppID: game.AppID, ManagedLaunch: game.ManagedLaunch, WrapperPath: game.WrapperPath, Mode: game.Mode.ResolveLegacyMode(),
	}
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
