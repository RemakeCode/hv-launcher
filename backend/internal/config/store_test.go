package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"hv-launcher/internal/linuwux"
	"hv-launcher/internal/model"
)

func TestStorePersistsAtomicallyAndMigratesVersionZero(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"games":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if store.Snapshot().Version != CurrentVersion {
		t.Fatalf("version was not migrated")
	}
	if err := store.PutGame(model.ManagedGame{AppID: "10", Name: "Game"}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if game, ok := reopened.Game("10"); !ok || game.Name != "Game" {
		t.Fatalf("persisted game missing: %+v, %v", game, ok)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temporary config remains: %v", err)
	}
}

func TestDataDirUsesXDGOrUserHome(t *testing.T) {
	if actual, err := DataDir("/home/deck", ""); err != nil || actual != "/home/deck/.local/share/hv-launcher" {
		t.Fatalf("default data directory = %q, %v", actual, err)
	}
	if actual, err := DataDir("/home/deck", "/data"); err != nil || actual != "/data/hv-launcher" {
		t.Fatalf("XDG data directory = %q, %v", actual, err)
	}
	if _, err := DataDir("/home/deck", "relative"); err == nil {
		t.Fatal("relative XDG_DATA_HOME was accepted")
	}
}

func TestStoreRollsBackMemoryWhenPersistenceFails(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	original := model.ManagedGame{AppID: "10", Name: "Original"}
	if err := store.PutGame(original); err != nil {
		t.Fatal(err)
	}
	store.path = filepath.Join(t.TempDir(), "missing", "config.json")

	if err := store.PutGame(model.ManagedGame{AppID: "20", Name: "New"}); err == nil {
		t.Fatal("put unexpectedly succeeded")
	}
	if _, ok := store.Game("20"); ok {
		t.Fatal("failed put remained in memory")
	}
	if err := store.PutGame(model.ManagedGame{AppID: "10", Name: "Changed"}); err == nil {
		t.Fatal("replacement unexpectedly succeeded")
	}
	if game, _ := store.Game("10"); game != original {
		t.Fatalf("failed replacement was not rolled back: %+v", game)
	}
	if err := store.DeleteGame("10"); err == nil {
		t.Fatal("delete unexpectedly succeeded")
	}
	if game, ok := store.Game("10"); !ok || game != original {
		t.Fatalf("failed delete was not rolled back: %+v, %v", game, ok)
	}
}

func TestOptionalModesRemainAbsentUntilExplicitlySaved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := []byte("{\n  \"version\": 1,\n  \"games\": {\n    \"10\": {\"appId\": \"10\", \"name\": \"Legacy\"}\n  }\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	game, ok := store.Game("10")
	if !ok || game.Mode.ResolveLegacyMode() != linuwux.ModeProton || game.Mode != "" {
		t.Fatalf("legacy mode = %q (resolved %q)", game.Mode, game.Mode.ResolveLegacyMode())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Fatalf("opening legacy config rewrote it:\n%s", after)
	}

	game.Mode = linuwux.ModeRuntime
	if err := store.PutGame(game); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if saved, _ := reopened.Game("10"); saved.Mode != linuwux.ModeRuntime {
		t.Fatalf("saved game mode = %q", saved.Mode)
	}
}

func TestInvalidModesAreRejectedAtWriteTime(t *testing.T) {
	for _, document := range []string{
		`{"version":1,"games":{"10":{"appId":"10","mode":"automatic"}}}`,
	} {
		t.Run(document, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			original := []byte(document)
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(dir); err != nil {
				t.Fatalf("opening manually edited configuration failed: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, original) {
				t.Fatal("invalid configuration was rewritten")
			}
		})
	}

	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutGame(model.ManagedGame{AppID: "10", Mode: "invalid"}); err == nil {
		t.Fatal("invalid game mode was persisted")
	}
}
