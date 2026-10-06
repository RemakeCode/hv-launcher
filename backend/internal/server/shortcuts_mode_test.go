package server

import (
	"context"
	"hv-launcher/internal/linuwux"
	"hv-launcher/internal/model"
	"net/http"
	"slices"
	"strings"
	"testing"
)

type testRuntimeManager struct {
	status     model.RuntimeStatus
	inspectErr error
}

func (m *testRuntimeManager) Inspect(context.Context) (model.RuntimeStatus, error) {
	return m.status, m.inspectErr
}
func availableTestRuntime() *testRuntimeManager {
	return &testRuntimeManager{status: model.RuntimeStatus{Supported: true, Available: true, State: model.RuntimeStateAvailable, Path: "/home/deck/.local/bin/linuwux", LibraryPath: "/home/deck/.local/share/linuwux/LinUwUx.so"}}
}
func TestPerGameModeValidation(t *testing.T) {
	service, _, store, _ := newTestService(t)
	runtimeManager := availableTestRuntime()
	service.options.Inspector.Runtime = runtimeManager

	fallback := perform(service.Handler(), http.MethodPost, "/v1/games/9/enable", `{"name":"Default Proton","shortcut":true,"currentLaunch":""}`)
	if fallback.Code != http.StatusOK {
		t.Fatalf("fallback enable returned %d: %s", fallback.Code, fallback.Body.String())
	}
	if game, _ := store.Game("9"); game.Mode != linuwux.ModeProton {
		t.Fatalf("fallback mode = %q, want %q", game.Mode, linuwux.ModeProton)
	}

	enabled := perform(service.Handler(), http.MethodPost, "/v1/games/10/enable", `{"name":"Runtime Game","shortcut":true,"currentLaunch":"%command%","mode":"runtime"}`)
	if enabled.Code != http.StatusOK {
		t.Fatalf("enable returned %d: %s", enabled.Code, enabled.Body.String())
	}
	game, _ := store.Game("10")
	if game.Mode != linuwux.ModeRuntime || !strings.Contains(game.ManagedLaunch, "linuwux' %command%") {
		t.Fatalf("managed game = %+v", game)
	}
	override := perform(service.Handler(), http.MethodPost, "/v1/games/11/enable", `{"name":"Proton Override","shortcut":true,"currentLaunch":"","mode":"proton"}`)
	if override.Code != http.StatusOK {
		t.Fatalf("override returned %d: %s", override.Code, override.Body.String())
	}
	if protonGame, _ := store.Game("11"); protonGame.Mode != linuwux.ModeProton || strings.Contains(protonGame.ManagedLaunch, "linuwux' %command%") {
		t.Fatalf("per-game override = %+v", protonGame)
	}
}

func TestUnavailableMethodIsRejectedWithoutPersistence(t *testing.T) {
	service, _, store, _ := newTestService(t)
	response := perform(service.Handler(), http.MethodPost, "/v1/games/10/enable", `{"name":"Game","shortcut":true,"currentLaunch":"","mode":"runtime"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("unavailable runtime returned %d: %s", response.Code, response.Body.String())
	}
	if _, ok := store.Game("10"); ok {
		t.Fatal("unavailable runtime mode was persisted")
	}
}

func TestRuntimeParamsAreSavedOnlyOnSuccessfulEnable(t *testing.T) {
	service, _, store, _ := newTestService(t)
	service.options.Inspector.Runtime = availableTestRuntime()
	original := `MANGOHUD=1 gamescope -f -- %command%`
	response := perform(service.Handler(), http.MethodPost, "/v1/games/10/enable", `{"name":"Game","shortcut":true,"currentLaunch":"MANGOHUD=1 gamescope -f -- %command%","mode":"runtime","params":["PROTON_AVX","LINUWUX_SYSCALL_HACK"]}`)
	if response.Code != http.StatusOK {
		t.Fatalf("enable returned %d: %s", response.Code, response.Body.String())
	}
	game, _ := store.Game("10")
	if !slices.Equal(game.Params, []string{"PROTON_AVX", "LINUWUX_SYSCALL_HACK"}) || game.OriginalLaunch != original || !strings.HasPrefix(game.ManagedLaunch, "MANGOHUD=1 PROTON_AVX=1 LINUWUX_SYSCALL_HACK=1 ") {
		t.Fatalf("managed game = %+v", game)
	}
	for _, body := range []string{
		`{"name":"Game","mode":"runtime","params":["LINUWUX_DEBUG"]}`,
		`{"name":"Game","mode":"proton","params":["PROTON_AVX"]}`,
		`{"name":"Game","mode":"runtime","currentLaunch":"PROTON_AVX=0 %command%","params":["PROTON_AVX"]}`,
	} {
		response := perform(service.Handler(), http.MethodPost, "/v1/games/11/enable", body)
		if response.Code != http.StatusConflict {
			t.Fatalf("invalid params returned %d: %s", response.Code, response.Body.String())
		}
		if _, exists := store.Game("11"); exists {
			t.Fatal("invalid params were persisted")
		}
	}
}
