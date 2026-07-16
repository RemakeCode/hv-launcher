package system

import (
	"context"
	"os"
	"os/exec"
)

type Reader interface {
	ReadFile(path string) ([]byte, error)
	ReadDir(path string) ([]os.DirEntry, error)
}

type OSReader struct{}

func (OSReader) ReadFile(path string) ([]byte, error)       { return os.ReadFile(path) }
func (OSReader) ReadDir(path string) ([]os.DirEntry, error) { return os.ReadDir(path) }

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type Paths struct {
	CPUInfo       string
	KernelRelease string
	DMIProduct    string
	DMIBoard      string
	DMIVendor     string
	ModulesRoot   string
	SteamRoots    []string
	RuntimeDir    string
}

func DefaultPaths(userHome, runtimeDir string) Paths {
	return Paths{
		CPUInfo:       "/proc/cpuinfo",
		KernelRelease: "/proc/sys/kernel/osrelease",
		DMIProduct:    "/sys/class/dmi/id/product_name",
		DMIBoard:      "/sys/class/dmi/id/board_name",
		DMIVendor:     "/sys/class/dmi/id/sys_vendor",
		ModulesRoot:   "/sys/module",
		SteamRoots: []string{
			userHome + "/.local/share/Steam",
			userHome + "/.steam/root",
			userHome + "/.var/app/com.valvesoftware.Steam/data/Steam",
		},
		RuntimeDir: runtimeDir,
	}
}
