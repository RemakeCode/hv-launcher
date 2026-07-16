package main

import (
	"strings"
	"testing"
)

func TestDefaultInvocationStartsServicePath(t *testing.T) {
	t.Setenv("DECKY_PLUGIN_RUNTIME_DIR", "")
	t.Setenv("DECKY_USER_HOME", "")

	err := run(nil)
	if err == nil || !strings.Contains(err.Error(), "DECKY_PLUGIN_RUNTIME_DIR") {
		t.Fatalf("default invocation returned %v", err)
	}
}

func TestServeSubcommandIsNotExposed(t *testing.T) {
	err := run([]string{"serve"})
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("serve subcommand returned %v", err)
	}
}
