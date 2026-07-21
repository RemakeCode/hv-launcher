package main

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"hv-launcher/internal/auth"
)

func TestDefaultInvocationStartsServicePath(t *testing.T) {
	t.Setenv("DECKY_PLUGIN_RUNTIME_DIR", "")
	t.Setenv("DECKY_USER_HOME", "")
	t.Setenv(auth.EnvironmentVariable, base64.RawURLEncoding.EncodeToString(make([]byte, auth.SecretBytes)))

	err := run(nil)
	if err == nil || !strings.Contains(err.Error(), "DECKY_PLUGIN_RUNTIME_DIR") {
		t.Fatalf("default invocation returned %v", err)
	}
	if _, present := os.LookupEnv(auth.EnvironmentVariable); present {
		t.Fatal("service startup left the setup secret in its environment")
	}
}

func TestServeSubcommandIsNotExposed(t *testing.T) {
	err := run([]string{"serve"})
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("serve subcommand returned %v", err)
	}
}

func TestRunWrappedRequiresCommandAfterSeparator(t *testing.T) {
	err := run([]string{"run", "--app-id", "1", "--"})
	if err == nil || err.Error() != "original command is required after --" {
		t.Fatalf("missing wrapped command returned %v", err)
	}
}
