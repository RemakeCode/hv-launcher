package server

import (
	"net/http"

	"hv-launcher/internal/auth"
	"hv-launcher/internal/model"
)

const moduleTestCapabilityBinding = "module-test"

type moduleTestRequest struct {
	Capability string `json:"capability"`
}

func (s *Service) testModule(w http.ResponseWriter, r *http.Request) {
	var request moduleTestRequest
	if !decodeStrict(w, r, &request) {
		return
	}
	if err := s.options.Capabilities.Consume(request.Capability, auth.OperationModuleTest, moduleTestCapabilityBinding); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}
	outcome, err := s.options.Controller.TestModule(r.Context())
	response := model.ModuleTestResponse{Outcome: outcome}

	if err != nil {
		response.Error = err.Error()
	}

	writeJSON(w, http.StatusOK, response)
}
