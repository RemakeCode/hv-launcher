package logger

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestConfigureInstallsSharedStructuredLogger(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	var output bytes.Buffer
	configured := Configure(&output)
	if slog.Default() != configured {
		t.Fatal("configured logger was not installed as the default")
	}
	slog.Info("backend event", "state", "ready")
	text := output.String()
	if !strings.Contains(text, "msg=\"backend event\"") || !strings.Contains(text, "state=ready") {
		t.Fatalf("unexpected structured log output: %q", text)
	}
}
