package server

import (
	"context"
	"errors"
	"net/http"

	"hv-launcher/internal/jobs"
	"hv-launcher/internal/linuwux"
	linuwuxruntime "hv-launcher/internal/linuwux/runtime"
	"hv-launcher/internal/model"
)

type runtimeInstallRequest struct {
	Action string `json:"action"`
}

func (s *Service) inspectRuntimeSetup(w http.ResponseWriter, r *http.Request) {
	status, err := s.options.Runtime.Inspect(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	result := model.RuntimeSetupStatus{Runtime: status, Repository: linuwuxruntime.Repository}
	if status.Supported {
		release, releaseErr := s.options.Runtime.Latest(r.Context())
		if releaseErr != nil {
			result.UpdateError = "The latest release could not be checked. The installed runtime was not changed."
		} else {
			result.Latest = &release
			result.Runtime.LatestVersion = release.Version
			if status.Available && status.VersionKnown {
				if linuwuxruntime.CompareVersions(status.Version, release.Version) < 0 {
					result.Runtime.UpdateState = model.RuntimeUpdateAvailable
				} else {
					result.Runtime.UpdateState = model.RuntimeUpdateCurrent
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Service) installRuntime(w http.ResponseWriter, r *http.Request) {
	var request runtimeInstallRequest
	if !decodeStrict(w, r, &request) {
		return
	}
	if request.Action != "install" && request.Action != "update" && request.Action != "repair" {
		writeError(w, http.StatusBadRequest, errors.New("runtime action must be install, update, or repair"))
		return
	}
	status, err := s.options.Runtime.Inspect(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !status.Supported {
		writeError(w, http.StatusUnprocessableEntity, errors.New("the prebuilt LinUwUx runtime is unsupported on this architecture"))
		return
	}

	started, err := s.options.Jobs.Start("runtime-install", "resolving-release", func(job *jobs.Job) (any, error) {
		result, installErr := s.options.Runtime.Install(context.Background(), func(update linuwuxruntime.Progress) {
			job.Update(update.Phase, update.Progress)
			job.Output(update.Message)
		})
		if installErr != nil {
			s.options.Logger.Error("LinUwUx runtime installation failed", "action", request.Action, "error", installErr)
			return nil, installErr
		}
		s.options.Logger.Info("LinUwUx runtime installation complete", "action", request.Action, "release", result.ReleaseTag)
		return result, nil
	})
	if err != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(err, jobs.ErrBusy) {
			statusCode = http.StatusConflict
		}
		writeError(w, statusCode, err)
		return
	}
	writeJSON(w, http.StatusAccepted, started)
}

func (s *Service) removeRuntime(w http.ResponseWriter, r *http.Request) {
	if active := s.options.Jobs.Active(); active.Active {
		writeError(w, http.StatusConflict, errors.New("finish the active setup operation before removing the runtime"))
		return
	}
	for _, game := range s.options.Config.Snapshot().Games {
		if game.Mode == linuwux.ModeRuntime {
			writeError(w, http.StatusConflict, errors.New("switch managed shortcuts to Proton or disable them before removing the runtime"))
			return
		}
	}
	if err := s.options.Runtime.Remove(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
