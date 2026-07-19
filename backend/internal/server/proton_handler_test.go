package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hv-launcher/internal/jobs"
)

func TestProtonPreflightUsesStrictRequest(t *testing.T) {
	service, root, _, _ := newTestService(t)
	missing := filepath.Join(root, "missing.tar.xz")
	response := perform(service.Handler(), http.MethodPost, "/v1/setup/proton/preflight", `{"path":`+jsonString(missing)+`}`)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing archive returned %d: %s", response.Code, response.Body.String())
	}

	unknownField := perform(service.Handler(), http.MethodPost, "/v1/setup/proton/preflight", `{"path":`+jsonString(missing)+`,"command":"tar"}`)
	if unknownField.Code != http.StatusBadRequest {
		t.Fatalf("unknown field returned %d: %s", unknownField.Code, unknownField.Body.String())
	}
}

func TestProtonInspectAndInstallAPI(t *testing.T) {
	service, root, _, _ := newTestService(t)
	archivePath := writeServerProtonArchive(t)
	inspectionResponse := perform(service.Handler(), http.MethodPost, "/v1/setup/proton/preflight", `{"path":`+jsonString(archivePath)+`}`)
	if inspectionResponse.Code != http.StatusOK {
		t.Fatalf("inspection returned %d: %s", inspectionResponse.Code, inspectionResponse.Body.String())
	}
	if strings.Contains(inspectionResponse.Body.String(), root) {
		t.Fatalf("inspection response exposed a destination path: %s", inspectionResponse.Body.String())
	}
	var inspected protonPreflightResponse
	if err := json.Unmarshal(inspectionResponse.Body.Bytes(), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.SelectionID == "" || inspected.Preflight.CompressedBytes <= 0 || len(inspected.Preflight.Destinations) != 1 || inspected.Responsibility == "" {
		t.Fatalf("unexpected preflight: %+v", inspected)
	}
	destinationID := inspected.Preflight.Destinations[0].ID
	installResponse := perform(service.Handler(), http.MethodPost, "/v1/setup/proton/install", `{"selectionId":`+jsonString(inspected.SelectionID)+`,"destinationId":`+jsonString(destinationID)+`,"confirmedSource":true}`)
	if installResponse.Code != http.StatusAccepted {
		t.Fatalf("install returned %d: %s", installResponse.Code, installResponse.Body.String())
	}
	var started jobs.JobSnapshot
	if err := json.Unmarshal(installResponse.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	terminal := waitForServerJob(t, service, started.ID)
	if terminal.State != jobs.JobSucceeded {
		t.Fatalf("installation failed: %+v", terminal)
	}
	finalRoot := filepath.Join(root, ".local", "share", "Steam", "compatibilitytools.d", "Proton-Test-LinUwUx")
	if _, err := os.Stat(filepath.Join(finalRoot, "proton")); err != nil {
		t.Fatalf("installed tool is absent: %v", err)
	}
	active := perform(service.Handler(), http.MethodGet, "/v1/setup/jobs/active", "")
	if active.Code != http.StatusOK || !strings.Contains(active.Body.String(), `"active":false`) {
		t.Fatalf("active snapshot = %d %s", active.Code, active.Body.String())
	}
}

func TestProtonInstallRejectsUnknownSelectionUnconfirmedSourceAndUnofferedDestination(t *testing.T) {
	service, _, _, _ := newTestService(t)
	unknown := perform(service.Handler(), http.MethodPost, "/v1/setup/proton/install", `{"selectionId":"unknown","destinationId":"native","confirmedSource":true}`)
	if unknown.Code != http.StatusGone {
		t.Fatalf("unknown inspection returned %d: %s", unknown.Code, unknown.Body.String())
	}

	archivePath := writeServerProtonArchive(t)
	response := perform(service.Handler(), http.MethodPost, "/v1/setup/proton/preflight", `{"path":`+jsonString(archivePath)+`}`)
	var inspected protonPreflightResponse
	if err := json.Unmarshal(response.Body.Bytes(), &inspected); err != nil {
		t.Fatal(err)
	}
	unconfirmed := perform(service.Handler(), http.MethodPost, "/v1/setup/proton/install", `{"selectionId":`+jsonString(inspected.SelectionID)+`,"destinationId":"native","confirmedSource":false}`)
	if unconfirmed.Code != http.StatusBadRequest {
		t.Fatalf("unconfirmed source returned %d: %s", unconfirmed.Code, unconfirmed.Body.String())
	}
	badDestinationID := "unknown"
	invalid := perform(service.Handler(), http.MethodPost, "/v1/setup/proton/install", `{"selectionId":`+jsonString(inspected.SelectionID)+`,"destinationId":`+jsonString(badDestinationID)+`,"confirmedSource":true}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("unreviewed destination returned %d: %s", invalid.Code, invalid.Body.String())
	}
}

func waitForServerJob(t *testing.T, service *Service, id string) jobs.JobSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		response := perform(service.Handler(), http.MethodGet, "/v1/setup/jobs/"+id, "")
		if response.Code != http.StatusOK {
			t.Fatalf("job snapshot returned %d: %s", response.Code, response.Body.String())
		}
		var snapshot jobs.JobSnapshot
		if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
			t.Fatal(err)
		}
		if snapshot.State != jobs.JobRunning {
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("setup job did not finish")
	return jobs.JobSnapshot{}
}

func writeServerProtonArchive(t *testing.T) string {
	t.Helper()
	root := "Proton-Test-LinUwUx"
	entries := []struct {
		name string
		mode int64
		data string
		dir  bool
	}{
		{name: root, mode: 0o755, dir: true},
		{name: root + "/compatibilitytool.vdf", mode: 0o644, data: fmt.Sprintf(`"compatibilitytools" { "compat_tools" { "%s" { "install_path" "." } } }`, root)},
		{name: root + "/proton", mode: 0o755, data: "launcher"},
		{name: root + "/toolmanifest.vdf", mode: 0o644, data: `"manifest" {}`},
		{name: root + "/version", mode: 0o644, data: "11-1\n"},
		{name: root + "/files", mode: 0o755, dir: true},
		{name: root + "/files/bin/tool", mode: 0o755, data: "payload"},
	}
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: entry.mode, Typeflag: tar.TypeReg, Size: int64(len(entry.data))}
		if entry.dir {
			header.Typeflag = tar.TypeDir
			header.Size = 0
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if entry.data != "" {
			if _, err := tarWriter.Write([]byte(entry.data)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "LinUwUx.tar.gz")
	if err := os.WriteFile(path, archive.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
