package server

import (
	"net/http"

	"hv-launcher/internal/cpuidmodule"
)

func (s *Service) inspectModuleRequirements(w http.ResponseWriter, r *http.Request) {
	preflight := s.options.ModulePreflight.Inspect(string(s.options.Controller.State()), cpuidmodule.Identity{})
	writeJSON(w, http.StatusOK, preflight)
}
