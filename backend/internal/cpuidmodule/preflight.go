package cpuidmodule

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type PreflightPaths struct {
	KernelRelease      string
	ModulesRoot        string
	BootRoot           string
	KernelLockdown     string
	OSRelease          string
	DKMSRoot           string
	DKMSExecutables    []string
	MakeExecutables    []string
	GCCExecutables     []string
	LDExecutables      []string
	ClangExecutables   []string
	LLDExecutables     []string
	LLVMExecutables    []string
	ModinfoExecutables []string
	DepmodExecutables  []string
	PackageManagers    map[string][]string
}

func DefaultPreflightPaths() PreflightPaths {
	return PreflightPaths{
		KernelRelease: "/proc/sys/kernel/osrelease", ModulesRoot: "/lib/modules", BootRoot: "/boot",
		KernelLockdown: "/sys/kernel/security/lockdown", OSRelease: "/etc/os-release", DKMSRoot: "/var/lib/dkms",
		DKMSExecutables: []string{"/usr/bin/dkms", "/usr/sbin/dkms"},
		MakeExecutables: []string{"/usr/bin/make"}, GCCExecutables: []string{"/usr/bin/gcc"},
		LDExecutables: []string{"/usr/bin/ld"}, ClangExecutables: []string{"/usr/bin/clang"},
		LLDExecutables: []string{"/usr/bin/ld.lld"}, LLVMExecutables: []string{"/usr/bin/llvm-nm"},
		ModinfoExecutables: []string{"/usr/sbin/modinfo", "/usr/bin/modinfo"},
		DepmodExecutables:  []string{"/usr/sbin/depmod", "/usr/bin/depmod"},
		PackageManagers: map[string][]string{
			"pacman": {"/usr/bin/pacman"}, "apt": {"/usr/bin/apt-get"}, "dnf": {"/usr/bin/dnf", "/usr/bin/dnf5"},
		},
	}
}

type PreflightInspector struct {
	Paths PreflightPaths
}

func NewPreflightInspector(paths PreflightPaths) *PreflightInspector {
	return &PreflightInspector{Paths: paths}
}

func (i *PreflightInspector) Inspect(controllerState string, identity Identity) Preflight {
	result := Preflight{ControllerState: controllerState, Checks: make([]PreflightCheck, 0, 8)}
	releaseData, err := readBoundedFile(i.Paths.KernelRelease, 4<<10)
	release := strings.TrimSpace(string(releaseData))
	result.KernelRelease = release
	result.add("running-kernel", err == nil && release != "", detail(err == nil && release != "", release, "Running kernel release is unavailable"))

	buildRoot := filepath.Join(i.Paths.ModulesRoot, release, "build")
	resolvedBuild, buildOK := matchingBuildRoot(buildRoot, release)
	if buildOK {
		result.BuildRoot = resolvedBuild
	}
	result.add("kernel-build", buildOK, detail(buildOK, resolvedBuild, "Matching build files for the running kernel are missing"))

	config, configPath, configOK := readKernelConfig(resolvedBuild, filepath.Join(i.Paths.BootRoot, "config-"+release))
	result.add("kernel-config", configOK, detail(configOK, configPath, "Running-kernel build configuration is unavailable"))
	clang := configUsesClang(config)
	if clang {
		result.Toolchain = "clang"
	} else {
		result.Toolchain = "gcc"
	}

	i.requireExecutable(&result, "dkms", "DKMS", i.Paths.DKMSExecutables)
	i.requireExecutable(&result, "make", "make", i.Paths.MakeExecutables)
	if clang {
		i.requireExecutable(&result, "clang", "Clang", i.Paths.ClangExecutables)
		i.requireExecutable(&result, "lld", "LLD", i.Paths.LLDExecutables)
		i.requireExecutable(&result, "llvm", "LLVM tools", i.Paths.LLVMExecutables)
	} else {
		i.requireExecutable(&result, "gcc", "GCC", i.Paths.GCCExecutables)
		i.requireExecutable(&result, "ld", "GNU ld", i.Paths.LDExecutables)
	}
	i.requireExecutable(&result, "modinfo", "modinfo", i.Paths.ModinfoExecutables)
	i.requireExecutable(&result, "depmod", "depmod", i.Paths.DepmodExecutables)

	result.DistributionID = readOSReleaseID(i.Paths.OSRelease)
	for _, manager := range []string{"pacman", "apt", "dnf"} {
		if firstExecutable(i.Paths.PackageManagers[manager]) != "" {
			result.PackageManager = manager
			break
		}
	}
	result.add("package-manager", result.PackageManager != "", detail(result.PackageManager != "", result.PackageManager, "No supported package manager was found; missing dependencies require manual installation"))

	result.Lockdown = readLockdown(i.Paths.KernelLockdown)
	result.add("lockdown", result.Lockdown != "unknown", detail(result.Lockdown != "unknown", result.Lockdown, "Kernel lockdown state could not be determined"))
	result.Signing = SigningEvidence{
		ModuleSigningEnabled: configEnabled(config, "CONFIG_MODULE_SIG"),
		SignatureForced:      configEnabled(config, "CONFIG_MODULE_SIG_FORCE"),
		TrustedKeysSetting:   configValue(config, "CONFIG_SYSTEM_TRUSTED_KEYS"),
	}
	if identity.PackageName != "" && identity.PackageVersion != "" {
		registration := filepath.Join(i.Paths.DKMSRoot, identity.PackageName, identity.PackageVersion)
		if info, err := os.Stat(registration); err == nil && info.IsDir() {
			result.DKMSRegistered = true
			source := filepath.Join(registration, "source")
			if resolved, err := filepath.EvalSymlinks(source); err == nil {
				result.RegisteredSource = resolved
			}
		}
	}
	result.add("controller", controllerState == "idle", detail(controllerState == "idle", "idle", "The hypervisor manager must be idle before module setup"))
	result.Ready = true
	for _, check := range result.Checks {
		if !check.OK && check.ID != "package-manager" && check.ID != "lockdown" {
			result.Ready = false
			break
		}
	}
	return result
}

func (p *Preflight) add(id string, ok bool, value string) {
	p.Checks = append(p.Checks, PreflightCheck{ID: id, OK: ok, Detail: value})
}

func (i *PreflightInspector) requireExecutable(result *Preflight, id, label string, paths []string) {
	executable := firstExecutable(paths)
	result.add(id, executable != "", detail(executable != "", executable, label+" is missing"))
}

func matchingBuildRoot(candidate, release string) (string, bool) {
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", false
	}
	data, err := readBoundedFile(filepath.Join(resolved, "include", "config", "kernel.release"), 4<<10)
	return resolved, err == nil && strings.TrimSpace(string(data)) == release
}

func readKernelConfig(buildRoot, bootConfig string) ([]byte, string, bool) {
	candidates := []string{bootConfig}
	if buildRoot != "" {
		candidates = append([]string{filepath.Join(buildRoot, ".config")}, candidates...)
	}
	for _, candidate := range candidates {
		data, err := readBoundedFile(candidate, 4<<20)
		if err == nil {
			return data, candidate, true
		}
	}
	return nil, "", false
}

func configUsesClang(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "CONFIG_CC_IS_CLANG=y" {
			return true
		}
		if strings.HasPrefix(line, "CONFIG_CLANG_VERSION=") {
			value, _ := strconv.Atoi(strings.TrimPrefix(line, "CONFIG_CLANG_VERSION="))
			if value > 0 {
				return true
			}
		}
	}
	return false
}

func configEnabled(data []byte, name string) bool {
	return configValue(data, name) == "y"
}

func configValue(data []byte, name string) string {
	prefix := name + "="
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.Trim(strings.TrimPrefix(line, prefix), "'\"")
		}
	}
	return ""
}

func firstExecutable(paths []string) string {
	for _, candidate := range paths {
		if executable, err := exec.LookPath(candidate); err == nil {
			return executable
		}
	}
	return ""
}

func readOSReleaseID(path string) string {
	info, err := os.Stat(path)
	if err != nil || info.Size() > 64<<10 {
		return ""
	}
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "ID=") {
			return strings.Trim(strings.TrimPrefix(line, "ID="), "'\"")
		}
	}
	return ""
}

func readLockdown(path string) string {
	data, err := readBoundedFile(path, 4<<10)
	if err != nil {
		return "unknown"
	}
	for _, state := range []string{"none", "integrity", "confidentiality"} {
		if strings.Contains(string(data), "["+state+"]") {
			return state
		}
	}
	value := strings.TrimSpace(string(data))
	if value == "none" || value == "integrity" || value == "confidentiality" {
		return value
	}
	return "unknown"
}

func readBoundedFile(path string, maximum int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("%s exceeds the %d-byte limit", path, maximum)
	}
	return data, nil
}

func detail(ok bool, success, failure string) string {
	if ok {
		return success
	}
	return failure
}

func (p Preflight) String() string {
	return fmt.Sprintf("kernel=%s ready=%t", p.KernelRelease, p.Ready)
}
