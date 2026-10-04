package server

import (
	"context"
	"hv-launcher/internal/linuwux"
	"hv-launcher/internal/model"
	"net/http"
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
