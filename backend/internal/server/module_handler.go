package server

import (
	"net/http"
)

func (s *Service) inspectModuleRequirements(w http.ResponseWriter, r *http.Request) {
	preflight := s.options.ModulePreflight.Inspect(string(s.options.Controller.State()))
	writeJSON(w, http.StatusOK, preflight)
}
