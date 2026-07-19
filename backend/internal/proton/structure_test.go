package proton

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectInstalledReusesProtonStructureRulesWithoutMutation(t *testing.T) {
	root := writeInstalledFixture(t, fixtureRoot)
	launcher := filepath.Join(root, "proton")
	before, err := os.Stat(launcher)
	if err != nil {
		t.Fatal(err)
	}

	tool, err := InspectInstalled(root)
	if err != nil {
		t.Fatal(err)
	}
	if tool.Name != fixtureRoot || tool.ManifestTool != fixtureRoot || tool.Payload != "files" {
		t.Fatalf("unexpected installed tool: %+v", tool)
	}
	after, err := os.Stat(launcher)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("read-only inspection changed launcher metadata: before=%v after=%v", before, after)
	}
}

func TestInspectInstalledRejectsIncompleteAndLinkedRequiredEntries(t *testing.T) {
	t.Run("missing payload", func(t *testing.T) {
		root := writeInstalledFixture(t, fixtureRoot)
		if err := os.RemoveAll(filepath.Join(root, "files")); err != nil {
			t.Fatal(err)
		}
		_, err := InspectInstalled(root)
		assertValidationError(t, err, ErrorInvalidLayout, "")
	})

	t.Run("linked launcher", func(t *testing.T) {
		root := writeInstalledFixture(t, fixtureRoot)
		launcher := filepath.Join(root, "proton")
		if err := os.Remove(launcher); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("version", launcher); err != nil {
			t.Fatal(err)
		}
		_, err := InspectInstalled(root)
		assertValidationError(t, err, ErrorInvalidLayout, "")
	})
}

func TestInspectInstalledBoundsManifest(t *testing.T) {
	root := writeInstalledFixture(t, fixtureRoot)
	limits := DefaultLimits()
	limits.MaxManifestBytes = 8
	_, err := InspectInstalledWithLimits(root, limits)
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Limit != "manifest-bytes" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeInstalledFixture(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(root, "files", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]struct {
		contents string
		mode     os.FileMode
	}{
		"compatibilitytool.vdf": {fixtureManifest, 0o644},
		"proton":                {"launcher", 0o755},
		"toolmanifest.vdf":      {`"manifest" {}`, 0o644},
		"version":               {"11-1\n", 0o644},
	}
	for relative, file := range files {
		if err := os.WriteFile(filepath.Join(root, relative), []byte(file.contents), file.mode); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
