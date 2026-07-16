package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"hv-launcher/internal/config"
	"hv-launcher/internal/hypervisor"
	"hv-launcher/internal/manage"
	"hv-launcher/internal/model"
	"hv-launcher/internal/system"
)

type testHost struct {
	mu     sync.Mutex
	loaded map[string]bool
	calls  []string
}

func (h *testHost) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, name+" "+strings.Join(args, " "))
	if name == "modinfo" {
		return []byte("6.18.0 SMP\n"), nil
	}
	if name == "modprobe" {
		if len(args) == 2 && args[0] == "-r" {
			h.loaded[args[1]] = false
		} else if len(args) == 1 {
			h.loaded[args[0]] = true
			if args[0] == "kvm_amd" {
				h.loaded["kvm"] = true
			}
		}
	}
	return nil, nil
}
func (h *testHost) LookPath(string) (string, error) { return "/sbin/modprobe", nil }
func (h *testHost) Loaded(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.loaded[name]
}
func (h *testHost) RefCount(string) int { return 0 }

type testJournal struct{ record *hypervisor.JournalRecord }

func (j *testJournal) Load() (*hypervisor.JournalRecord, error)    { return j.record, nil }
func (j *testJournal) Write(record hypervisor.JournalRecord) error { j.record = &record; return nil }
func (j *testJournal) Clear() error                                { j.record = nil; return nil }

func newTestService(t *testing.T) (*Service, *config.Store, *hypervisor.Controller) {
	t.Helper()
	root := t.TempDir()
	settings := filepath.Join(root, "settings")
	store, err := config.Open(settings)
	if err != nil {
		t.Fatal(err)
	}
	steamRoot := filepath.Join(root, "Steam")
	if err := os.MkdirAll(filepath.Join(steamRoot, "compatibilitytools.d", "GE-Proton11-1-LinUwUx"), 0o755); err != nil {
		t.Fatal(err)
	}
	cpu := filepath.Join(root, "cpuinfo")
	kernel := filepath.Join(root, "osrelease")
	writeServerFile(t, cpu, "processor : 0\nvendor_id : AuthenticAMD\ncpu family : 23\nmodel : 49\nmodel name : AMD Ryzen\nflags :\n\n")
	writeServerFile(t, kernel, "6.18.0\n")
	modules := filepath.Join(root, "modules")
	host := &testHost{loaded: map[string]bool{"kvm": true, "kvm_amd": true}}
	controller, err := hypervisor.New(hypervisor.Options{Runner: host, Modules: host, Journal: &testJournal{}, KernelRelease: "6.18.0", EffectiveUID: func() int { return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	inspector := &system.Inspector{Reader: system.OSReader{}, Runner: host, Paths: system.Paths{
		CPUInfo: cpu, KernelRelease: kernel, ModulesRoot: modules, SteamRoots: []string{steamRoot},
	}}
	manager := &manage.Manager{Store: store, WrapperPath: "/home/deck/.local/share/hv-launcher/hv-launcher-wrapper"}
	service, err := New(Options{ListenAddress: "127.0.0.1:42991", Config: store, Inspector: inspector, Manager: manager, Controller: controller, ProcessReader: system.OSReader{}, ProcRoot: filepath.Join(root, "proc")})
	if err != nil {
		t.Fatal(err)
	}
	return service, store, controller
}

func TestHandlerRoutesAndStrictRequests(t *testing.T) {
	service, store, _ := newTestService(t)
	for _, path := range []string{"/v1/status", "/v1/config"} {
		response := perform(service.Handler(), http.MethodGet, path, "")
		if response.Code != http.StatusOK {
			t.Fatalf("%s returned %d: %s", path, response.Code, response.Body.String())
		}
	}
	removed := perform(service.Handler(), http.MethodPost, "/v1/games/restore-all", `{}`)
	if removed.Code != http.StatusNotFound {
		t.Fatalf("removed restore-all route returned %d", removed.Code)
	}
	malformed := perform(service.Handler(), http.MethodPost, "/v1/sessions", `{"appId":"10","module":"evil"}`)
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("unknown field returned %d: %s", malformed.Code, malformed.Body.String())
	}
	unknown := perform(service.Handler(), http.MethodPost, "/v1/sessions", `{"appId":"999"}`)
	if unknown.Code != http.StatusForbidden {
		t.Fatalf("unknown App ID returned %d", unknown.Code)
	}
	if err := store.PutGame(model.ManagedGame{AppID: "10", Name: "Test", ManagedLaunch: "managed"}); err != nil {
		t.Fatal(err)
	}
	valid := perform(service.Handler(), http.MethodPost, "/v1/sessions", `{"appId":"10"}`)
	if valid.Code != http.StatusOK {
		t.Fatalf("valid session returned %d: %s", valid.Code, valid.Body.String())
	}
	var result model.SessionStartResponse
	if err := json.Unmarshal(valid.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	ended := perform(service.Handler(), http.MethodDelete, "/v1/sessions/"+result.SessionID, "")
	if ended.Code != http.StatusNoContent {
		t.Fatalf("end returned %d: %s", ended.Code, ended.Body.String())
	}
}

func TestDeckyCORSPreflight(t *testing.T) {
	service, _, _ := newTestService(t)
	request := httptest.NewRequest(http.MethodOptions, "/v1/lifetime", nil)
	request.Header.Set("Origin", deckyOrigin)
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "content-type")
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("preflight returned %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != deckyOrigin {
		t.Fatalf("allow origin = %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type" {
		t.Fatalf("allow headers = %q", got)
	}
}

func TestDeckyCORSRejectsOtherBrowserOrigins(t *testing.T) {
	service, _, _ := newTestService(t)
	request := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	request.Header.Set("Origin", "https://example.com")
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("foreign origin returned %d: %s", response.Code, response.Body.String())
	}
}

func TestEnableDisableAndConflict(t *testing.T) {
	service, _, _ := newTestService(t)
	enabled := perform(service.Handler(), http.MethodPost, "/v1/games/10/enable", `{"name":"Frontend Game","shortcut":true,"currentLaunch":"MANGOHUD=1 %command%"}`)
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable returned %d: %s", enabled.Code, enabled.Body.String())
	}
	var managed model.ManageGameResponse
	if err := json.Unmarshal(enabled.Body.Bytes(), &managed); err != nil {
		t.Fatal(err)
	}
	conflict := perform(service.Handler(), http.MethodPost, "/v1/games/10/disable", `{"currentLaunch":"user changed"}`)
	if conflict.Code != http.StatusOK || !strings.Contains(conflict.Body.String(), `"conflict":true`) {
		t.Fatalf("conflict returned %d: %s", conflict.Code, conflict.Body.String())
	}
	restored := perform(service.Handler(), http.MethodPost, "/v1/games/10/disable", `{"currentLaunch":`+jsonString(managed.ManagedLaunch)+`}`)
	if restored.Code != http.StatusOK || !strings.Contains(restored.Body.String(), `MANGOHUD=1 %command%`) {
		t.Fatalf("restore returned %d: %s", restored.Code, restored.Body.String())
	}
}

func TestEnableRejectsMalformedFrontendMetadata(t *testing.T) {
	service, _, _ := newTestService(t)
	tests := []string{
		`{"name":"","shortcut":true,"currentLaunch":""}`,
		`{"name":"Game","shortcut":true,"currentLaunch":"","target":"/bin/evil"}`,
	}
	for _, payload := range tests {
		response := perform(service.Handler(), http.MethodPost, "/v1/games/10/enable", payload)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("payload %s returned %d: %s", payload, response.Code, response.Body.String())
		}
	}
}

func TestConcurrentSessionCallsAreSerialized(t *testing.T) {
	service, store, controller := newTestService(t)
	if err := store.PutGame(model.ManagedGame{AppID: "10", Name: "Test"}); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errorsFound := make(chan error, 10)
	for index := 0; index < 10; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			response := perform(service.Handler(), http.MethodPost, "/v1/sessions", `{"appId":"10"}`)
			if response.Code != http.StatusOK {
				errorsFound <- errors.New(response.Body.String())
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatal(err)
	}
	if len(controller.Sessions()) != 10 {
		t.Fatalf("got %d sessions", len(controller.Sessions()))
	}
}

func TestLoopbackBindingIsMandatory(t *testing.T) {
	service, _, _ := newTestService(t)
	options := service.options
	for _, address := range []string{"0.0.0.0:42991", "[::1]:42991", "localhost:42991", ""} {
		options.ListenAddress = address
		if _, err := New(options); err == nil {
			t.Fatalf("accepted %q", address)
		}
	}
}

func TestRequestSizeLimit(t *testing.T) {
	service, _, _ := newTestService(t)
	payload := `{"appId":"10","padding":"` + strings.Repeat("x", maxRequestBytes) + `"}`
	response := perform(service.Handler(), http.MethodPost, "/v1/sessions", payload)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("got %d", response.Code)
	}
}

func perform(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func writeServerFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func jsonString(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
