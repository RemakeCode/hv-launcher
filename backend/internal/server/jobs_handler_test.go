package server

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hv-launcher/internal/jobs"
)

func TestSetupEventEndpointIsReadOnlyAndCORSAccessible(t *testing.T) {
	service, _, _, _ := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/v1/setup/events", nil).WithContext(ctx)
	request.Header.Set("Origin", deckyOrigin)
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/event-stream" || response.Header().Get("Access-Control-Allow-Origin") != deckyOrigin {
		t.Fatalf("event response = %d %+v", response.Code, response.Header())
	}
	if mutation := perform(service.Handler(), http.MethodPost, "/v1/setup/events", `{}`); mutation.Code != http.StatusMethodNotAllowed {
		t.Fatalf("event mutation returned %d", mutation.Code)
	}
}

func TestSetupEventJSONContract(t *testing.T) {
	service, _, _, _ := newTestService(t)
	server := httptest.NewServer(service.Handler())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/setup/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("event stream returned %d", response.StatusCode)
	}

	startUpdates := make(chan struct{})
	stopUpdates := make(chan struct{})
	defer close(stopUpdates)
	if _, err := service.options.Jobs.Start("proton-install", "starting", func(job *jobs.Job) (any, error) {
		<-startUpdates
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		progress := 0
		for {
			select {
			case <-ticker.C:
				progress = (progress + 1) % 100
				job.Update("installing", progress)
			case <-stopUpdates:
				return map[string]any{"toolName": "test", "destinationId": "native", "sha256": "hash", "restartSteam": true}, nil
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	activeResponse := perform(service.Handler(), http.MethodGet, "/v1/setup/jobs/active", "")
	if activeResponse.Code != http.StatusOK {
		t.Fatalf("active job returned %d: %s", activeResponse.Code, activeResponse.Body.String())
	}
	active := requireJSONObject(t, activeResponse.Body.Bytes(), "active", "job")
	requireJobSnapshotContract(t, active["job"])
	close(startUpdates)

	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		event := requireJSONObject(t, []byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), "type", "job")
		if eventType := requireJSONStringField(t, event, "type"); eventType != "setup-job" {
			t.Fatalf("event type = %q", eventType)
		}
		requireJobSnapshotContract(t, event["job"])
		break
	}
}
