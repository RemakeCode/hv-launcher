package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Service) activeSetupJob(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.options.Jobs.Active())
}

func (s *Service) setupJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "jobID")
	if id == "" || len(id) > 128 {
		writeError(w, http.StatusBadRequest, errors.New("invalid setup job ID"))
		return
	}
	snapshot, ok := s.options.Jobs.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("setup job was not found"))
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Service) setupEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming is unavailable"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	events, unsubscribe := s.options.Jobs.Subscribe()
	defer unsubscribe()
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-events:
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: setup-job\ndata: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
