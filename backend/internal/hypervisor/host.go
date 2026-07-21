package hypervisor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
	LookPath(name string) (string, error)
}

type ExecRunner struct{}

type ModuleState interface {
	Loaded(name string) bool
	RefCount(name string) int
}

type Reader interface {
	ReadFile(path string) ([]byte, error)
	ReadDir(path string) ([]os.DirEntry, error)
}

type SysModuleState struct {
	Reader Reader
	Root   string
}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = []string{
		"HOME=/root",
		"LANG=C",
		"LC_ALL=C",
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
	}

	return command.CombinedOutput()
}

func (ExecRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (s SysModuleState) Loaded(name string) bool {
	_, err := s.Reader.ReadDir(filepath.Join(s.Root, name))
	return err == nil
}

func (s SysModuleState) RefCount(name string) int {
	data, err := s.Reader.ReadFile(filepath.Join(s.Root, name, "refcnt"))
	if err != nil {
		return 0
	}
	value, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return value
}
