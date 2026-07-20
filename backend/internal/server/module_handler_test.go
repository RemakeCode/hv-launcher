package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"hv-launcher/internal/auth"
	"hv-launcher/internal/cpuidmodule"
)

func TestModulePreflightRequiresExactOneUseCapability(t *testing.T) {
	service, root, _, _ := newTestService(t)
	archivePath := writeServerModuleZIP(t, root, false)
	requestWithoutCapability := `{"path":` + jsonString(archivePath) + `,"capability":""}`
	response := perform(service.Handler(), http.MethodPost, "/v1/setup/module/preflight", requestWithoutCapability)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unsigned preflight returned %d: %s", response.Code, response.Body.String())
	}

	capability := signServerCapability(t, auth.OperationModulePreflight, archivePath)
	request := `{"path":` + jsonString(archivePath) + `,"capability":` + jsonString(capability) + `}`
	response = perform(service.Handler(), http.MethodPost, "/v1/setup/module/preflight", request)
	if response.Code != http.StatusOK {
		t.Fatalf("authorized preflight returned %d: %s", response.Code, response.Body.String())
	}
	var preflight cpuidmodule.ArchivePreflight
	if err := json.Unmarshal(response.Body.Bytes(), &preflight); err != nil {
		t.Fatal(err)
	}
	if preflight.FileName != filepath.Base(archivePath) || preflight.CompressedBytes <= 0 || preflight.System.KernelRelease == "" {
		t.Fatalf("unexpected preflight: %+v", preflight)
	}

	replay := perform(service.Handler(), http.MethodPost, "/v1/setup/module/preflight", request)
	if replay.Code != http.StatusForbidden {
		t.Fatalf("replayed preflight returned %d: %s", replay.Code, replay.Body.String())
	}
}

func TestModulePreflightRejectsCallerFieldsButDefersDeepValidation(t *testing.T) {
	service, root, _, _ := newTestService(t)
	unknown := perform(service.Handler(), http.MethodPost, "/v1/setup/module/preflight", `{"path":"/tmp/source.zip","capability":"x","command":"id"}`)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("inspection accepted caller command: %d %s", unknown.Code, unknown.Body.String())
	}

	archivePath := writeServerModuleZIP(t, root, true)
	capability := signServerCapability(t, auth.OperationModulePreflight, archivePath)
	request := `{"path":` + jsonString(archivePath) + `,"capability":` + jsonString(capability) + `}`
	response := perform(service.Handler(), http.MethodPost, "/v1/setup/module/preflight", request)
	if response.Code != http.StatusOK {
		t.Fatalf("fast preflight rejected structurally unsafe archive: %d: %s", response.Code, response.Body.String())
	}
}

func writeServerModuleZIP(t *testing.T, root string, unsafe bool) string {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entries := map[string]string{
		"dkms.conf":         "PACKAGE_NAME=\"cpuid_fault_emulation\"\nPACKAGE_VERSION=\"0.1\"\nBUILT_MODULE_NAME=\"cpuid_fault_emulation\"\nDEST_MODULE_LOCATION=\"/updates\"\nAUTOINSTALL=\"yes\"\n",
		"Makefile":          "obj-m += cpuid_fault_emulation.o\n",
		"inc/vmcb_layout.h": "header\n", "inc/host_state.h": "header\n",
		"src/cpuid_fault_emulation.c": "source\n", "src/capture_context.S": "source\n", "src/run_vm.S": "source\n",
	}
	for name, contents := range entries {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if unsafe {
		header := &zip.FileHeader{Name: "link"}
		header.SetMode(os.ModeSymlink | 0o777)
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte("Makefile")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(root, "cpuid_fault_emulation.zip")
	if err := os.WriteFile(archivePath, buffer.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return archivePath
}
