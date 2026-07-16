package wrapper

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"hv-launcher/internal/model"
)

func TestInstallCreatesStableExecutableCopy(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(t.TempDir(), "service")
	if err := os.WriteFile(source, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	destination, err := Install(source, home)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "binary" {
		t.Fatalf("copy: %q, %v", data, err)
	}
	info, err := os.Stat(destination)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("mode: %v, %v", info, err)
	}
	if second, err := Install(source, home); err != nil || second != destination {
		t.Fatalf("idempotent install failed: %s, %v", second, err)
	}
}

func TestServiceUnavailableFailsOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	output := filepath.Join(t.TempDir(), "ran")
	err := Run(context.Background(), Options{
		AppID: "10", BaseURL: "http://127.0.0.1:1/v1", HTTPTimeout: 100 * time.Millisecond,
		Command: []string{"/bin/sh", "-c", "printf ran > " + shellQuoteTest(output)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(output); err != nil || string(data) != "ran" {
		t.Fatalf("original command did not run: %q, %v", data, err)
	}
}

func TestExplicitActivationFailureDoesNotStartChild(t *testing.T) {
	output := filepath.Join(t.TempDir(), "must-not-run")
	restore := useTransport(t, func(*http.Request) (*http.Response, error) {
		return response(http.StatusConflict, `{"error":"hypervisor transition requires root"}`), nil
	})
	defer restore()
	err := Run(context.Background(), Options{AppID: "10", BaseURL: "http://service/v1", HTTPTimeout: time.Second, Command: []string{"/bin/sh", "-c", "touch " + shellQuoteTest(output)}})
	if err == nil {
		t.Fatal("expected activation failure")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "hypervisor transition requires root" {
		t.Fatalf("backend error was not preserved: %T %v", err, err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("child ran after explicit failure: %v", err)
	}
}

func TestSuccessfulSessionWrapsChildAndCleansUp(t *testing.T) {
	var mu sync.Mutex
	started, ended := false, false
	restore := useTransport(t, func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPost {
			var request model.SessionStartRequest
			_ = json.NewDecoder(r.Body).Decode(&request)
			started = request.AppID == "10"
			return response(http.StatusOK, `{"sessionId":"session"}`), nil
		}
		if r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/session") {
			ended = true
			return response(http.StatusNoContent, ""), nil
		}
		return response(http.StatusNotFound, ""), nil
	})
	defer restore()
	if err := Run(context.Background(), Options{AppID: "10", BaseURL: "http://service/v1", HTTPTimeout: time.Second, Command: []string{"/bin/sh", "-c", "exit 0"}}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !started || !ended {
		t.Fatalf("started=%v ended=%v", started, ended)
	}
}

func TestChildExitCodeIsPreserved(t *testing.T) {
	err := runChild(context.Background(), []string{"/bin/sh", "-c", "exit 23"})
	type exitCoder interface{ ExitCode() int }
	exitError, ok := err.(exitCoder)
	if !ok || exitError.ExitCode() != 23 {
		t.Fatalf("got %T %v", err, err)
	}
}

func TestTerminationSignalIsForwardedToChildProcessGroup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix process-group fixture")
	}
	ready := filepath.Join(t.TempDir(), "ready")
	done := make(chan error, 1)
	go func() {
		done <- runChild(context.Background(), []string{
			"/bin/sh", "-c", "trap 'exit 0' TERM; printf ready > " + shellQuoteTest(ready) + "; while :; do sleep 0.05; done",
		})
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child never became ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child did not exit cleanly after forwarded signal: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("forwarded signal did not stop child process group")
	}
}

func shellQuoteTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func useTransport(t *testing.T, function roundTripFunc) func() {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = function
	return func() { http.DefaultTransport = previous }
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status, Status: http.StatusText(status), Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(body)),
	}
}
