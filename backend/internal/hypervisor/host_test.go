package hypervisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecRunnerDoesNotInheritDynamicLibraryEnvironment(t *testing.T) {
	t.Setenv("LD_LIBRARY_PATH", "/steam/runtime")
	t.Setenv("LD_PRELOAD", "/steam/runtime/lib.so")

	output, err := (ExecRunner{}).Run(context.Background(), "/usr/bin/env")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(output), "LD_LIBRARY_PATH=") || strings.Contains(string(output), "LD_PRELOAD=") {
		t.Fatalf("root command inherited dynamic-library environment: %s", output)
	}
}

func TestExecRunnerLookupDoesNotUseInheritedPath(t *testing.T) {
	directory := t.TempDir()
	name := "hv-launcher-untrusted-modprobe"
	if err := os.WriteFile(filepath.Join(directory, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)

	if executable, err := (ExecRunner{}).LookPath(name); err == nil {
		t.Fatalf("found inherited-PATH executable %q", executable)
	}
}
