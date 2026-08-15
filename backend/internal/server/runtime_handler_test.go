package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"hv-launcher/internal/jobs"
	"hv-launcher/internal/linuwux"
	linuwuxruntime "hv-launcher/internal/linuwux/runtime"
	"hv-launcher/internal/model"
)

type testRuntimeManager struct {
	status     model.RuntimeStatus
	inspectErr error
	release    model.RuntimeRelease
	releaseErr error
	install    model.RuntimeInstallResult
	installErr error
	removed    bool
	removeErr  error
}

func (m *testRuntimeManager) Inspect(context.Context) (model.RuntimeStatus, error) {
	return m.status, m.inspectErr
}

func (m *testRuntimeManager) Latest(context.Context) (model.RuntimeRelease, error) {
	return m.release, m.releaseErr
}

func (m *testRuntimeManager) Install(_ context.Context, progress linuwuxruntime.ProgressFunc) (model.RuntimeInstallResult, error) {
	if progress != nil {
		progress(linuwuxruntime.Progress{
			Phase: "downloading", Progress: 45, Message: "Downloading liblinuwux.so",
			ReleaseTag: m.release.Tag, Asset: linuwuxruntime.LibraryAsset,
		})
	}
	return m.install, m.installErr
}

func (m *testRuntimeManager) Remove(context.Context) error {
	m.removed = true
	return m.removeErr
}

func TestRuntimeSetupInspectionAndGenericJobProgress(t *testing.T) {
	service, _, _, _ := newTestService(t)
	runtimeManager := availableTestRuntime("26.08.14")
	runtimeManager.release = model.RuntimeRelease{
		Repository: linuwuxruntime.Repository, Tag: "v26.08.14.1", Version: "26.08.14.1",
	}
	runtimeManager.install = model.RuntimeInstallResult{
		Repository: linuwuxruntime.Repository, ReleaseTag: "v26.08.14.1", Version: "26.08.14.1",
		Path: "/home/deck/.local/bin/linuwux", LibraryPath: "/home/deck/.local/lib/liblinuwux.so",
	}
	service.options.Runtime = runtimeManager
	service.options.Inspector.Runtime = runtimeManager

	response := perform(service.Handler(), http.MethodGet, "/v1/setup/runtime", "")
	if response.Code != http.StatusOK {
		t.Fatalf("inspect returned %d: %s", response.Code, response.Body.String())
	}
	var setup model.RuntimeSetupStatus
	if err := json.Unmarshal(response.Body.Bytes(), &setup); err != nil {
		t.Fatal(err)
	}
	if setup.Repository != linuwuxruntime.Repository || setup.Runtime.UpdateState != model.RuntimeUpdateAvailable || setup.Latest == nil {
		t.Fatalf("setup = %+v", setup)
	}

	started := perform(service.Handler(), http.MethodPost, "/v1/setup/runtime", `{"action":"update"}`)
	if started.Code != http.StatusAccepted {
		t.Fatalf("install returned %d: %s", started.Code, started.Body.String())
	}
	var snapshot jobs.JobSnapshot
	if err := json.Unmarshal(started.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	finished := waitForServerJob(t, service, snapshot.ID)
	if finished.State != jobs.JobSucceeded || len(finished.Output) == 0 || finished.Output[len(finished.Output)-1] != "Downloading liblinuwux.so" {
		t.Fatalf("finished job = %+v", finished)
	}
	result, ok := finished.Result.(map[string]any)
	if !ok || result["releaseTag"] != "v26.08.14.1" || result["repository"] != linuwuxruntime.Repository {
		t.Fatalf("runtime result = %+v", finished.Result)
	}
}

func TestRuntimeSetupKeepsInstalledRuntimeAvailableWhenUpdateCheckFails(t *testing.T) {
	service, _, _, _ := newTestService(t)
	runtimeManager := availableTestRuntime("26.08.14.1")
	runtimeManager.releaseErr = errors.New("offline")
	service.options.Runtime = runtimeManager
	service.options.Inspector.Runtime = runtimeManager

	response := perform(service.Handler(), http.MethodGet, "/v1/setup/runtime", "")
	var setup model.RuntimeSetupStatus
	if err := json.Unmarshal(response.Body.Bytes(), &setup); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !setup.Runtime.Available || setup.Runtime.UpdateState != model.RuntimeUpdateUnknown || setup.UpdateError == "" {
		t.Fatalf("setup = %+v, status = %d", setup, response.Code)
	}
}

func TestRuntimeRemoval(t *testing.T) {
	service, _, _, _ := newTestService(t)
	runtimeManager := availableTestRuntime("26.08.14.1")
	service.options.Runtime = runtimeManager

	response := perform(service.Handler(), http.MethodDelete, "/v1/setup/runtime", "")
	if response.Code != http.StatusNoContent || !runtimeManager.removed {
		t.Fatalf("remove returned %d: %s, removed=%v", response.Code, response.Body.String(), runtimeManager.removed)
	}
}

func TestRuntimeRemovalRejectsManagedRuntimeShortcuts(t *testing.T) {
	service, _, store, _ := newTestService(t)
	runtimeManager := availableTestRuntime("26.08.14.1")
	service.options.Runtime = runtimeManager
	if err := store.PutGame(model.ManagedGame{AppID: "10", Mode: linuwux.ModeRuntime}); err != nil {
		t.Fatal(err)
	}

	response := perform(service.Handler(), http.MethodDelete, "/v1/setup/runtime", "")
	if response.Code != http.StatusConflict || runtimeManager.removed {
		t.Fatalf("remove returned %d: %s, removed=%v", response.Code, response.Body.String(), runtimeManager.removed)
	}
}

func TestPerGameModeValidation(t *testing.T) {
	service, _, store, _ := newTestService(t)
	runtimeManager := availableTestRuntime("26.08.14.1")
	service.options.Runtime = runtimeManager
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

func TestSoleRuntimeMethodIsSelectedAutomatically(t *testing.T) {
	service, _, store, _ := newTestService(t)
	runtimeManager := availableTestRuntime("26.08.14.1")
	service.options.Runtime = runtimeManager
	service.options.Inspector.Runtime = runtimeManager
	service.options.Inspector.Paths.SteamRoots = nil

	response := perform(service.Handler(), http.MethodPost, "/v1/games/10/enable", `{"name":"Runtime Only","shortcut":true,"currentLaunch":""}`)
	if response.Code != http.StatusOK {
		t.Fatalf("enable returned %d: %s", response.Code, response.Body.String())
	}
	game, _ := store.Game("10")
	if game.Mode != linuwux.ModeRuntime {
		t.Fatalf("mode = %q", game.Mode)
	}
}

func TestMethodReconfigurationRejectsExternalSteamEdit(t *testing.T) {
	service, _, store, _ := newTestService(t)
	runtimeManager := availableTestRuntime("26.08.14.1")
	service.options.Runtime = runtimeManager
	service.options.Inspector.Runtime = runtimeManager

	enabled := perform(service.Handler(), http.MethodPost, "/v1/games/10/enable", `{"name":"Game","shortcut":true,"currentLaunch":"%command%","mode":"proton"}`)
	if enabled.Code != http.StatusOK {
		t.Fatal(enabled.Body.String())
	}
	before, _ := store.Game("10")
	conflict := perform(service.Handler(), http.MethodPost, "/v1/games/10/mode", `{"currentLaunch":"externally edited","mode":"runtime"}`)
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict returned %d: %s", conflict.Code, conflict.Body.String())
	}
	unchanged, _ := store.Game("10")
	if unchanged != before {
		t.Fatalf("record changed after conflict: %+v", unchanged)
	}

	payload := `{"currentLaunch":` + jsonString(before.ManagedLaunch) + `,"mode":"runtime"}`
	changed := perform(service.Handler(), http.MethodPost, "/v1/games/10/mode", payload)
	if changed.Code != http.StatusOK {
		t.Fatalf("change returned %d: %s", changed.Code, changed.Body.String())
	}
	after, _ := store.Game("10")
	if after.Mode != linuwux.ModeRuntime || after.OriginalLaunch != before.OriginalLaunch || after.ManagedLaunch == before.ManagedLaunch {
		t.Fatalf("reconfigured game = %+v", after)
	}
}

func TestRuntimeInspectionDoesNotRewriteManagedGames(t *testing.T) {
	service, _, store, _ := newTestService(t)
	enabled := perform(service.Handler(), http.MethodPost, "/v1/games/10/enable", `{"name":"Existing","shortcut":true,"currentLaunch":"%command%","mode":"proton"}`)
	if enabled.Code != http.StatusOK {
		t.Fatal(enabled.Body.String())
	}
	before, _ := store.Game("10")
	runtimeManager := availableTestRuntime("26.08.14.1")
	runtimeManager.release = model.RuntimeRelease{Repository: linuwuxruntime.Repository, Tag: "v26.08.14.1", Version: "26.08.14.1"}
	service.options.Runtime = runtimeManager
	service.options.Inspector.Runtime = runtimeManager

	if response := perform(service.Handler(), http.MethodGet, "/v1/setup/runtime", ""); response.Code != http.StatusOK {
		t.Fatal(response.Body.String())
	}
	after, _ := store.Game("10")
	if after != before {
		t.Fatalf("managed game was rewritten: before=%+v after=%+v", before, after)
	}
}

func availableTestRuntime(version string) *testRuntimeManager {
	return &testRuntimeManager{status: model.RuntimeStatus{
		Supported: true, Available: true, State: model.RuntimeStateAvailable,
		Path: "/home/deck/.local/bin/linuwux", LibraryPath: "/home/deck/.local/lib/liblinuwux.so",
		Version: version, VersionKnown: true, UpdateState: model.RuntimeUpdateUnknown,
	}}
}
