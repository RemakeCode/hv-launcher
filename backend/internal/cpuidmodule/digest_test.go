package cpuidmodule

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizedSourceDigestMatchesEquivalentDirectory(t *testing.T) {
	entries := validModuleEntries()
	archivePath := writeModuleZIP(t, entries)
	inspection, err := NewInspector().ValidatePath(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		path := filepath.Join(root, filepath.FromSlash(entry.name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(entry.content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	digest, err := DigestDirectory(root, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if digest != inspection.SourceDigest {
		t.Fatalf("directory digest %s != archive digest %s", digest, inspection.SourceDigest)
	}

	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := DigestDirectory(root, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if changed == digest {
		t.Fatal("content change did not change normalized source digest")
	}
}
