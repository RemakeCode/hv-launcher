package runtime

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExternalInstallationDetectionDoesNotRunWrapper(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("x86-64 runtime")
	}
	home := t.TempDir()
	inspector, err := NewInspector(home)
	if err != nil {
		t.Fatal(err)
	}
	status, err := inspector.Inspect(context.Background())
	if err != nil || status.Available || status.State != "absent" {
		t.Fatalf("absent: %+v %v", status, err)
	}
	marker := filepath.Join(home, "wrapper-ran")
	for _, path := range []string{status.Path, status.LibraryPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(status.Path, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	status, _ = inspector.Inspect(context.Background())
	if status.Available || status.State != "invalid" {
		t.Fatal("incomplete runtime accepted")
	}
	if err := os.WriteFile(status.LibraryPath, []byte("library fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	status, _ = inspector.Inspect(context.Background())
	if !status.Available || status.LibraryPath != filepath.Join(home, ".local/share/linuwux/LinUwUx.so") {
		t.Fatalf("current layout: %+v", status)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("wrapper executed during inspection")
	}
	if err := os.Chmod(status.Path, 0644); err != nil {
		t.Fatal(err)
	}
	status, _ = inspector.Inspect(context.Background())
	if status.Available {
		t.Fatal("nonexecutable wrapper accepted")
	}
}
