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

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type ExecRunner struct{}

type Paths struct {
	CPUInfo        string
	KernelRelease  string
	DMIProduct     string
	DMIBoard       string
	DMIVendor      string
	ModulesRoot    string
	KernelLockdown string
	SteamRoots     []string
}

func (OSReader) ReadFile(path string) ([]byte, error)       { return os.ReadFile(path) }
func (OSReader) ReadDir(path string) ([]os.DirEntry, error) { return os.ReadDir(path) }

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

func DefaultPaths(userHome string) Paths {
	return Paths{
		CPUInfo:        "/proc/cpuinfo",
		KernelRelease:  "/proc/sys/kernel/osrelease",
		DMIProduct:     "/sys/class/dmi/id/product_name",
		DMIBoard:       "/sys/class/dmi/id/board_name",
		DMIVendor:      "/sys/class/dmi/id/sys_vendor",
		ModulesRoot:    "/sys/module",
		KernelLockdown: "/sys/kernel/security/lockdown",
		SteamRoots: []string{
			userHome + "/.local/share/Steam",
			userHome + "/.steam/root",
			userHome + "/.var/app/com.valvesoftware.Steam/data/Steam",
		},
	}
}
