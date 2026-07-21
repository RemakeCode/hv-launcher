package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"hv-launcher/internal/model"
)

type Options struct {
	AppID       string
	BaseURL     string
	Command     []string
	HTTPTimeout time.Duration
}

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("session activation failed (%d): %s", e.StatusCode, e.Message)
}

func Run(ctx context.Context, options Options) error {
	client := &http.Client{Timeout: options.HTTPTimeout}
	sessionID, err := startSession(ctx, client, options.BaseURL, options.AppID)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			return apiErr
		}
		// Fail open only when the lifecycle service cannot be reached.
		return runChild(ctx, options.Command)
	}

	childErr := runChild(ctx, options.Command)
	endErr := endSession(context.Background(), client, options.BaseURL, sessionID)
	if childErr != nil {
		return childErr
	}
	return endErr
}

func startSession(ctx context.Context, client *http.Client, baseURL, appID string) (string, error) {
	body, err := json.Marshal(model.SessionStartRequest{AppID: appID})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/sessions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", apiErrorFromResponse(response)
	}

	var result model.SessionStartResponse
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", &APIError{StatusCode: response.StatusCode, Message: "invalid session response: " + err.Error()}
	}
	if result.SessionID == "" {
		return "", &APIError{StatusCode: response.StatusCode, Message: "service returned an empty session ID"}
	}
	return result.SessionID, nil
}

func endSession(ctx context.Context, client *http.Client, baseURL, sessionID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimRight(baseURL, "/")+"/sessions/"+sessionID, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return apiErrorFromResponse(response)
	}
	return nil
}

func apiErrorFromResponse(response *http.Response) *APIError {
	message := response.Status
	data, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err == nil {
		var payload struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &payload) == nil && strings.TrimSpace(payload.Error) != "" {
			message = strings.TrimSpace(payload.Error)
		} else if body := strings.TrimSpace(string(data)); body != "" {
			message = body
		}
	}
	return &APIError{StatusCode: response.StatusCode, Message: message}
}

func runChild(ctx context.Context, command []string) error {
	if len(command) == 0 {
		return errors.New("original command is empty")
	}

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}

	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for received := range signals {
			_ = syscall.Kill(-cmd.Process.Pid, received.(syscall.Signal))
		}
	}()
	err := cmd.Wait()
	signal.Stop(signals)
	close(signals)
	<-done
	return err
}
