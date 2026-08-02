package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"hv-launcher/internal/auth"
	"hv-launcher/internal/cpuidmodule"
	"hv-launcher/internal/model"
)

func TestModulePreflightReturnsHostRequirementsWithoutArchiveInput(t *testing.T) {
	service, _, _, _ := newTestService(t)
	response := perform(service.Handler(), http.MethodGet, "/v1/setup/module/preflight", "")
	if response.Code != http.StatusOK {
		t.Fatalf("module preflight returned %d: %s", response.Code, response.Body.String())
	}
	var preflight cpuidmodule.Preflight
	if err := json.Unmarshal(response.Body.Bytes(), &preflight); err != nil {
		t.Fatal(err)
	}
	if preflight.KernelRelease == "" || len(preflight.Checks) == 0 {
		t.Fatalf("unexpected preflight: %+v", preflight)
	}
}

func TestModuleInstallRejectsCallerProvidedFields(t *testing.T) {
	requests := []struct {
		name    string
		request string
	}{
		{name: "confirmation", request: `{"path":"/tmp/source.zip","capability":"","confirmedSource":true}`},
		{name: "dependency plan", request: `{"path":"/tmp/source.zip","dependencyPlan":{},"capability":""}`},
	}
	for _, test := range requests {
		t.Run(test.name, func(t *testing.T) {
			service, _, _, _ := newTestService(t)
			response := perform(service.Handler(), http.MethodPost, "/v1/setup/module/install", test.request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("caller-provided field returned %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestModuleTestEndpointUsesFixedControllerOperation(t *testing.T) {
	service, _, _, _ := newTestService(t)
	missing := perform(service.Handler(), http.MethodPost, "/v1/setup/module/test", `{"capability":""}`)
	if missing.Code != http.StatusForbidden {
		t.Fatalf("module test without capability returned %d: %s", missing.Code, missing.Body.String())
	}
	capability := signServerCapability(t, auth.OperationModuleTest, moduleTestCapabilityBinding)
	response := perform(service.Handler(), http.MethodPost, "/v1/setup/module/test", `{"capability":"`+capability+`"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("module test returned %d: %s", response.Code, response.Body.String())
	}
	var result model.ModuleTestResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Outcome.State != model.ModuleVerificationVerified {
		t.Fatalf("unexpected module test outcome: %+v", result)
	}
	unknown := perform(service.Handler(), http.MethodPost, "/v1/setup/module/test", `{"module":"evil"}`)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("caller-controlled module parameter returned %d", unknown.Code)
	}
}
