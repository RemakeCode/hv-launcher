package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"hv-launcher/internal/model"
)

type Inspector struct{ UserHome string }

func NewInspector(userHome string) (*Inspector, error) {
	if userHome == "" || !filepath.IsAbs(userHome) {
		return nil, errors.New("Decky user home must be an absolute path")
	}
	return &Inspector{UserHome: filepath.Clean(userHome)}, nil
}

// Inspect reads the external installation without running its wrapper or downloading files.
func (i *Inspector) Inspect(_ context.Context) (model.RuntimeStatus, error) {
	status := model.RuntimeStatus{
		Supported:   runtime.GOARCH == "amd64",
		Path:        filepath.Join(i.UserHome, ".local/bin/linuwux"),
		LibraryPath: filepath.Join(i.UserHome, ".local/share/linuwux/LinUwUx.so"),
		State:       model.RuntimeStateAbsent,
		Detail:      "LinUwUx runtime is not installed. Follow the upstream installation instructions, then refresh.",
	}
	if !status.Supported {
		status.State = model.RuntimeStateUnsupported
		status.Detail = "LinUwUx runtime supports x86-64 only"
		return status, nil
	}
	wrapper, wrapperErr := os.Stat(status.Path)
	library, libraryErr := os.Stat(status.LibraryPath)
	if errors.Is(wrapperErr, os.ErrNotExist) && errors.Is(libraryErr, os.ErrNotExist) {
		return status, nil
	}
	status.State = model.RuntimeStateInvalid
	status.Detail = "The LinUwUx wrapper or library is missing or unusable. Follow upstream installation instructions."
	if wrapperErr != nil || libraryErr != nil {
		return status, nil
	}
	if !wrapper.Mode().IsRegular() || wrapper.Mode().Perm()&0111 == 0 || wrapper.Size() == 0 {
		return status, nil
	}
	if !library.Mode().IsRegular() || library.Mode().Perm()&0444 == 0 || library.Size() == 0 {
		return status, nil
	}
	status.Available = true
	status.State = model.RuntimeStateAvailable
	status.Detail = "Externally installed LinUwUx runtime detected"
	return status, nil
}
