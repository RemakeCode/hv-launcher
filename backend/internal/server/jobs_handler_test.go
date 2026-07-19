package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
