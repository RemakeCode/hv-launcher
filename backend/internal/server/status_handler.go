package server

import "net/http"

func (s *Service) status(w http.ResponseWriter, r *http.Request) {
	status, err := s.options.Inspector.Inspect(r.Context(), string(s.options.Controller.State()))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}
