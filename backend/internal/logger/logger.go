package logger

import (
	"io"
	"log/slog"
	"os"
)

// Configure installs the backend's shared structured logger. Decky captures
// the child process's inherited stdout, so Python does not need to proxy logs.
func Configure(output io.Writer) *slog.Logger {
	if output == nil {
		output = os.Stdout
	}
	appLogger := slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(appLogger)
	return appLogger
}
