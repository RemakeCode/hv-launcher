package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"hv-launcher/internal/cpuidmodule"
)

func TestModulePreflightReturnsHostRequirementsWithoutArchiveInput(t *testing.T) {
	service, _, _, _ := newTestService(t)
	response := perform(service.Handler(), http.MethodGet, "/v1/setup/module/preflight", "")
	if response.Code != http.StatusOK {
		t.Fatalf("module preflight returned %d: %s", response.Code, response.Body.String())
	}
	contract := requireJSONObject(t, response.Body.Bytes(), "ready", "kernelRelease", "lockdown", "controllerState", "checks")
	checks := requireJSONArrayField(t, contract, "checks")
	if len(checks) == 0 {
		t.Fatal("module preflight response has no checks")
	}
	requireJSONObject(t, checks[0], "id", "ok", "detail")
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
