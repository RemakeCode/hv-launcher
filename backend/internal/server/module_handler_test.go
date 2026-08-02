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

func TestModuleInstallRejectsCallerProvidedConfirmation(t *testing.T) {
	service, _, _, _ := newTestService(t)
	response := perform(service.Handler(), http.MethodPost, "/v1/setup/module/install", `{"path":"/tmp/source.zip","capability":"","confirmedSource":true}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("caller-provided confirmation returned %d: %s", response.Code, response.Body.String())
	}
}

func TestModuleInstallRejectsCallerProvidedDependencyPlan(t *testing.T) {
	service, _, _, _ := newTestService(t)
	response := perform(service.Handler(), http.MethodPost, "/v1/setup/module/install", `{"path":"/tmp/source.zip","dependencyPlan":{},"capability":""}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("caller-provided dependency plan returned %d: %s", response.Code, response.Body.String())
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
