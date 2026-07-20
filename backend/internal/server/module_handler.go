package server

import (
	"errors"
	"net/http"
	"unicode/utf8"

	"hv-launcher/internal/auth"
	"hv-launcher/internal/cpuidmodule"
)

type modulePreflightRequest struct {
	Path       string `json:"path"`
	Capability string `json:"capability"`
}

func (s *Service) preflightModuleArchive(w http.ResponseWriter, r *http.Request) {
	var request modulePreflightRequest
	if !decodeStrict(w, r, &request) {
		return
	}
	if request.Path == "" || !utf8.ValidString(request.Path) || len(request.Path) > cpuidmodule.MaxSelectedPathBytes {
		writeError(w, http.StatusBadRequest, errors.New("a bounded absolute module archive path is required"))
		return
	}
	if err := s.options.Capabilities.Consume(request.Capability, auth.OperationModulePreflight, request.Path); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	preflight, err := s.options.ModuleInspector.PreflightPath(request.Path)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	preflight.System = s.options.ModulePreflight.Inspect(string(s.options.Controller.State()), cpuidmodule.Identity{})
	writeJSON(w, http.StatusOK, preflight)
}
