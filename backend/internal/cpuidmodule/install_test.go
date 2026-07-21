package cpuidmodule

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type installRunner struct {
	calls           [][]string
	moduleAvailable bool
	buildError      bool
	addError        bool
	installError    bool
	removeError     bool
	kernelRelease   string
}

func TestInstallerRegistersBuildsInstallsAndVerifiesCurrentKernel(t *testing.T) {
	archivePath := writeModuleZIP(t, validModuleEntries())
	paths := installerPaths(t)
	runner := &installRunner{}
	installer := NewInstaller(paths, runner)
	result, err := installer.Install(context.Background(), archivePath, nil, func(string, int, string) {})
	if err != nil {
		t.Fatal(err)
	}
	if result.ModuleName != "cpuid_fault_emulation" || result.KernelRelease != "6.18.7-test" ||
		result.NoOp || result.SigningRequired || !strings.HasPrefix(result.Vermagic, "6.18.7-test") {
		t.Fatalf("unexpected install result: %+v", result)
	}
	if !runner.called("add") || !runner.called("build") || !runner.called("install") {
		t.Fatalf("DKMS stages were not run: %+v", runner.calls)
	}
	for _, call := range runner.calls {
		if call[0] == "add" && (len(call) != 2 || filepath.Base(call[1]) != "source" || call[1] == archivePath) {
			t.Fatalf("DKMS did not receive private extracted source: %v", call)
		}
	}
	for _, call := range runner.calls {
		if call[0] == "build" || call[0] == "install" {
			if !contains(call, "-k") || !contains(call, "6.18.7-test") || !contains(call, "--force") {
				t.Fatalf("DKMS call was not bound to running kernel: %v", call)
			}
		}
	}
}

func TestStageSelectedArchiveEnforcesCompressedLimit(t *testing.T) {
	selected := filepath.Join(t.TempDir(), "selected.zip")
	writePreflightFile(t, selected, "12345", 0o644)
	staged := filepath.Join(t.TempDir(), "staged.zip")
	if err := stageSelectedArchive(selected, staged, 4); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("oversized selected archive error = %v", err)
	}
}

func TestExtractSelectedArchiveEnforcesFileLimit(t *testing.T) {
	archivePath := writeModuleZIP(t, validModuleEntries())
	limits := DefaultLimits()
	limits.MaxFileBytes = 1
	if err := extractSelectedArchive(archivePath, t.TempDir(), limits); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("oversized extracted file error = %v", err)
	}
}

func TestInstallerDoesNotReplaceExistingCompatibleModule(t *testing.T) {
	archivePath := writeModuleZIP(t, validModuleEntries())
	paths := installerPaths(t)
	runner := &installRunner{}
	runner.moduleAvailable = true
	result, err := NewInstaller(paths, runner).Install(context.Background(), archivePath, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.NoOp || runner.called("add") || runner.called("build") || runner.called("install") {
		t.Fatalf("compatible module was not a no-op: result=%+v calls=%v", result, runner.calls)
	}
}

func TestInstallerRejectsExistingRegistrationWithoutReplacingIt(t *testing.T) {
	archivePath := writeModuleZIP(t, validModuleEntries())
	paths := installerPaths(t)
	registration := filepath.Join(paths.DKMSRoot, "cpuid_fault_emulation", "0.1")
	if err := os.MkdirAll(registration, 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &installRunner{}
	_, err := NewInstaller(paths, runner).Install(context.Background(), archivePath, nil, nil)
	if !errors.Is(err, ErrModuleConflict) || runner.called("add") {
		t.Fatalf("existing registration was not protected: error=%v calls=%v", err, runner.calls)
	}
}

func TestInstallerRemovesOnlyOwnedRegistrationAfterBuildFailure(t *testing.T) {
	archivePath := writeModuleZIP(t, validModuleEntries())
	paths := installerPaths(t)
	runner := &installRunner{buildError: true}
	_, err := NewInstaller(paths, runner).Install(context.Background(), archivePath, nil, nil)
	if err == nil || !runner.called("remove") {
		t.Fatalf("failed build did not trigger owned cleanup: error=%v calls=%v", err, runner.calls)
	}
}

func TestInstallerRemovesOnlyOwnedRegistrationAfterInstallFailure(t *testing.T) {
	archivePath := writeModuleZIP(t, validModuleEntries())
	paths := installerPaths(t)
	runner := &installRunner{installError: true}
	_, err := NewInstaller(paths, runner).Install(context.Background(), archivePath, nil, nil)
	if err == nil || !runner.called("remove") {
		t.Fatalf("failed install did not trigger owned cleanup: error=%v calls=%v", err, runner.calls)
	}
}

func TestInstallerDoesNotCleanRegistrationWhenAddFails(t *testing.T) {
	archivePath := writeModuleZIP(t, validModuleEntries())
	paths := installerPaths(t)
	runner := &installRunner{addError: true}
	_, err := NewInstaller(paths, runner).Install(context.Background(), archivePath, nil, nil)
	if err == nil || runner.called("remove") {
		t.Fatalf("failed add changed registration ownership: error=%v calls=%v", err, runner.calls)
	}
}

func TestInstallerReportsOwnedCleanupFailure(t *testing.T) {
	archivePath := writeModuleZIP(t, validModuleEntries())
	paths := installerPaths(t)
	runner := &installRunner{buildError: true, removeError: true}
	_, err := NewInstaller(paths, runner).Install(context.Background(), archivePath, nil, nil)
	if !errors.Is(err, ErrModuleCleanup) {
		t.Fatalf("cleanup failure was not retained: %v", err)
	}
}

func TestInstallerReportsSigningRequiredWhenLockdownEnforcesIt(t *testing.T) {
	archivePath := writeModuleZIP(t, validModuleEntries())
	paths := installerPaths(t)
	writePreflightFile(t, paths.KernelLockdown, "none integrity [integrity] confidentiality\n", 0o644)
	result, err := NewInstaller(paths, &installRunner{}).Install(context.Background(), archivePath, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.SigningRequired || result.Signer != "" {
		t.Fatalf("unsigned lockdown result was not reported: %+v", result)
	}
}

func TestInstallerBuildsTheSameSourceForAChangedRunningKernel(t *testing.T) {
	archivePath := writeModuleZIP(t, validModuleEntries())
	paths := installerPathsForRelease(t, "6.19.0-test")
	runner := &installRunner{kernelRelease: "6.19.0-test"}
	result, err := NewInstaller(paths, runner).Install(context.Background(), archivePath, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.KernelRelease != "6.19.0-test" {
		t.Fatalf("module was not bound to the new running kernel: %+v", result)
	}
	for _, call := range runner.calls {
		if call[0] == "build" || call[0] == "install" {
			if !contains(call, "6.19.0-test") {
				t.Fatalf("DKMS call used the wrong kernel: %v", call)
			}
		}
	}
}

func installerPaths(t *testing.T) PreflightPaths {
	return installerPathsForRelease(t, "6.18.7-test")
}

func installerPathsForRelease(t *testing.T, release string) PreflightPaths {
	t.Helper()
	root := t.TempDir()
	kernelPath := filepath.Join(root, "proc", "osrelease")
	dkms := filepath.Join(root, "usr", "bin", "dkms")
	modinfo := filepath.Join(root, "usr", "bin", "modinfo")
	writePreflightFile(t, kernelPath, release+"\n", 0o644)
	writeExecutable(t, dkms)
	writeExecutable(t, modinfo)
	writePreflightFile(t, filepath.Join(root, "lockdown"), "[none] integrity confidentiality\n", 0o644)
	return PreflightPaths{
		KernelRelease: kernelPath, KernelLockdown: filepath.Join(root, "lockdown"),
		DKMSRoot:        filepath.Join(root, "var", "lib", "dkms"),
		DKMSExecutables: []string{dkms}, ModinfoExecutables: []string{modinfo},
	}
}

func (r *installRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
	if len(args) > 0 && args[0] == "install" {
		r.moduleAvailable = true
		if r.installError {
			return []byte("install failed"), errors.New("install failed")
		}
	}
	if len(args) >= 1 && args[0] == "add" && r.addError {
		return []byte("add failed"), errors.New("add failed")
	}
	if len(args) >= 1 && args[0] == "build" && r.buildError {
		return []byte("compiler failed"), errors.New("build failed")
	}
	if len(args) >= 1 && args[0] == "remove" {
		if r.removeError {
			return []byte("remove failed"), errors.New("remove failed")
		}
		return []byte("removed"), nil
	}
	if len(args) >= 4 && args[0] == "-k" && args[2] == "-F" {
		release := r.kernelRelease
		if release == "" {
			release = "6.18.7-test"
		}
		switch args[3] {
		case "name":
			if !r.moduleAvailable {
				return nil, errors.New("module is absent")
			}
			return []byte("cpuid_fault_emulation\n"), nil
		case "filename":
			return []byte("/lib/modules/" + release + "/updates/cpuid_fault_emulation.ko\n"), nil
		case "vermagic":
			return []byte(release + " SMP mod_unload\n"), nil
		case "signer":
			return []byte("\n"), nil
		}
	}
	return []byte("ok"), nil
}

func (r *installRunner) called(stage string) bool {
	for _, call := range r.calls {
		if len(call) > 0 && call[0] == stage {
			return true
		}
	}
	return false
}
