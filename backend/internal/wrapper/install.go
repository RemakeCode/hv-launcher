package wrapper

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func Install(sourceExecutable, userHome string) (string, error) {
	directory := filepath.Join(userHome, ".local", "share", "hv-launcher")
	destination := filepath.Join(directory, "hv-launcher-wrapper")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", err
	}
	sourceData, err := os.ReadFile(sourceExecutable)
	if err != nil {
		return "", fmt.Errorf("read wrapper source: %w", err)
	}
	if existing, err := os.ReadFile(destination); err == nil && bytes.Equal(existing, sourceData) {
		if err := os.Chmod(destination, 0o755); err != nil {
			return "", err
		}
		return destination, setOwnerFromHome(destination, userHome)
	}
	temporary := destination + ".tmp"
	input, err := os.Open(sourceExecutable)
	if err != nil {
		return "", err
	}
	defer input.Close()
	output, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return "", err
	}
	if err := output.Sync(); err != nil {
		output.Close()
		return "", err
	}
	if err := output.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temporary, destination); err != nil {
		return "", err
	}
	if err := setOwnerFromHome(destination, userHome); err != nil {
		return "", err
	}
	return destination, nil
}

func setOwnerFromHome(path, userHome string) error {
	info, err := os.Stat(userHome)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if err := os.Chown(filepath.Dir(path), int(stat.Uid), int(stat.Gid)); err != nil {
		return err
	}
	return os.Chown(path, int(stat.Uid), int(stat.Gid))
}
