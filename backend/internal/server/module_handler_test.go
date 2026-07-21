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
	var preflight cpuidmodule.Preflight
	if err := json.Unmarshal(response.Body.Bytes(), &preflight); err != nil {
		t.Fatal(err)
	}
	if preflight.KernelRelease == "" || len(preflight.Checks) == 0 {
		t.Fatalf("unexpected preflight: %+v", preflight)
	}
}

func TestModulePreflightDoesNotAcceptArchiveInput(t *testing.T) {
	service, _, _, _ := newTestService(t)
	response := perform(service.Handler(), http.MethodPost, "/v1/setup/module/preflight", `{"path":"/tmp/source.zip"}`)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("module preflight accepted archive input: %d %s", response.Code, response.Body.String())
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
