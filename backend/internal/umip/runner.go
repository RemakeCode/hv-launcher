package umip

import (
	"bytes"
	"context"
	"os/exec"
)

const maxUpdaterOutputBytes = 64 << 10

type safeExecRunner struct{}

func (safeExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = []string{
		"HOME=/root",
		"LANG=C",
		"LC_ALL=C",
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
	}
	output := &boundedOutput{maximum: maxUpdaterOutputBytes}
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	return output.Bytes(), err
}

type boundedOutput struct {
	buffer    bytes.Buffer
	maximum   int
	truncated bool
}

func (w *boundedOutput) Write(data []byte) (int, error) {
	original := len(data)
	remaining := w.maximum - w.buffer.Len()
	if remaining > 0 {
		if len(data) > remaining {
			data = data[:remaining]
			w.truncated = true
		}
		_, _ = w.buffer.Write(data)
	} else if len(data) > 0 {
		w.truncated = true
	}
	return original, nil
}

func (w *boundedOutput) Bytes() []byte {
	result := append([]byte(nil), w.buffer.Bytes()...)
	if w.truncated {
		result = append(result, []byte("\n[output truncated]")...)
	}
	return result
}
