package hypervisor

import (
	"context"
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
