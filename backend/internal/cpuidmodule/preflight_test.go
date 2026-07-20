package cpuidmodule

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreflightFindsReadyRunningKernelAndGCCRequirements(t *testing.T) {
	paths := readyPreflightPaths(t, false)
	preflight := NewPreflightInspector(paths).Inspect("idle", testModuleIdentity("0.1"))
	if !preflight.Ready || preflight.KernelRelease != "6.18.7-test" || preflight.Toolchain != "gcc" ||
		preflight.PackageManager != "pacman" || preflight.DistributionID != "cachyos" || preflight.Lockdown != "none" ||
		!preflight.Signing.ModuleSigningEnabled || preflight.Signing.SignatureForced ||
		preflight.Signing.TrustedKeysSetting != "certs/signing_key.pem" {
		t.Fatalf("unexpected preflight: %+v", preflight)
	}
	for _, check := range preflight.Checks {
		if !check.OK {
			t.Fatalf("unexpected failed check: %+v", check)
		}
	}
}

func TestPreflightRequiresClangToolchainForClangKernel(t *testing.T) {
	paths := readyPreflightPaths(t, true)
	for _, executable := range append(append([]string{}, paths.ClangExecutables...), append(paths.LLDExecutables, paths.LLVMExecutables...)...) {
		_ = os.Remove(executable)
	}
	preflight := NewPreflightInspector(paths).Inspect("idle", testModuleIdentity("0.1"))
	if preflight.Ready || preflight.Toolchain != "clang" || checkOK(preflight, "clang") || checkOK(preflight, "lld") || checkOK(preflight, "llvm") {
		t.Fatalf("missing Clang requirements passed: %+v", preflight)
	}
	for _, executable := range []string{paths.ClangExecutables[0], paths.LLDExecutables[0], paths.LLVMExecutables[0]} {
		writeExecutable(t, executable)
	}
	if ready := NewPreflightInspector(paths).Inspect("idle", testModuleIdentity("0.1")); !ready.Ready {
		t.Fatalf("complete Clang requirements failed: %+v", ready)
	}
}

func TestPreflightBlocksKernelMismatchAndActiveControllerButNotMissingPackageManager(t *testing.T) {
	paths := readyPreflightPaths(t, false)
	for _, candidates := range paths.PackageManagers {
		for _, executable := range candidates {
			_ = os.Remove(executable)
		}
	}
	withoutManager := NewPreflightInspector(paths).Inspect("idle", testModuleIdentity("0.1"))
	if !withoutManager.Ready || checkOK(withoutManager, "package-manager") {
		t.Fatalf("package manager incorrectly blocked complete preflight: %+v", withoutManager)
	}

	buildRelease := filepath.Join(paths.ModulesRoot, "6.18.7-test", "build", "include", "config", "kernel.release")
	writePreflightFile(t, buildRelease, "another-kernel\n", 0o644)
	mismatch := NewPreflightInspector(paths).Inspect("idle", testModuleIdentity("0.1"))
	if mismatch.Ready || checkOK(mismatch, "kernel-build") {
		t.Fatalf("mismatched build tree passed: %+v", mismatch)
	}
	writePreflightFile(t, buildRelease, "6.18.7-test\n", 0o644)
	active := NewPreflightInspector(paths).Inspect("active", testModuleIdentity("0.1"))
	if active.Ready || checkOK(active, "controller") {
		t.Fatalf("active controller passed: %+v", active)
	}
}

func TestPreflightReportsExistingDKMSRegistration(t *testing.T) {
	paths := readyPreflightPaths(t, false)
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	registration := filepath.Join(paths.DKMSRoot, "cpuid_fault_emulation", "0.1")
	if err := os.MkdirAll(registration, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, filepath.Join(registration, "source")); err != nil {
		t.Fatal(err)
	}
	preflight := NewPreflightInspector(paths).Inspect("idle", testModuleIdentity("0.1"))
	if !preflight.DKMSRegistered || preflight.RegisteredSource != source {
		t.Fatalf("registration not reported: %+v", preflight)
	}
}

func TestPreflightUsesArchiveDeclaredDKMSVersion(t *testing.T) {
	paths := readyPreflightPaths(t, false)
	registration := filepath.Join(paths.DKMSRoot, "cpuid_fault_emulation", "1.0")
	if err := os.MkdirAll(registration, 0o755); err != nil {
		t.Fatal(err)
	}
	preflight := NewPreflightInspector(paths).Inspect("idle", testModuleIdentity("1.0"))
	if !preflight.DKMSRegistered {
		t.Fatalf("versioned registration not reported: %+v", preflight)
	}
}

func testModuleIdentity(version string) Identity {
	return Identity{PackageName: "cpuid_fault_emulation", PackageVersion: version}
}

func readyPreflightPaths(t *testing.T, clang bool) PreflightPaths {
	t.Helper()
	root := t.TempDir()
	release := "6.18.7-test"
	paths := PreflightPaths{
		KernelRelease: filepath.Join(root, "proc", "osrelease"), ModulesRoot: filepath.Join(root, "lib", "modules"),
		BootRoot: filepath.Join(root, "boot"), KernelLockdown: filepath.Join(root, "lockdown"),
		OSRelease: filepath.Join(root, "os-release"), DKMSRoot: filepath.Join(root, "var", "lib", "dkms"),
		DKMSExecutables:    []string{filepath.Join(root, "usr", "bin", "dkms")},
		MakeExecutables:    []string{filepath.Join(root, "usr", "bin", "make")},
		GCCExecutables:     []string{filepath.Join(root, "usr", "bin", "gcc")},
		LDExecutables:      []string{filepath.Join(root, "usr", "bin", "ld")},
		ClangExecutables:   []string{filepath.Join(root, "usr", "bin", "clang")},
		LLDExecutables:     []string{filepath.Join(root, "usr", "bin", "ld.lld")},
		LLVMExecutables:    []string{filepath.Join(root, "usr", "bin", "llvm-nm")},
		ModinfoExecutables: []string{filepath.Join(root, "usr", "bin", "modinfo")},
		DepmodExecutables:  []string{filepath.Join(root, "usr", "bin", "depmod")},
		PackageManagers:    map[string][]string{"pacman": {filepath.Join(root, "usr", "bin", "pacman")}},
	}
	writePreflightFile(t, paths.KernelRelease, release+"\n", 0o644)
	buildRoot := filepath.Join(paths.ModulesRoot, release, "build")
	writePreflightFile(t, filepath.Join(buildRoot, "include", "config", "kernel.release"), release+"\n", 0o644)
	config := "CONFIG_CC_IS_GCC=y\nCONFIG_MODULE_SIG=y\nCONFIG_SYSTEM_TRUSTED_KEYS=\"certs/signing_key.pem\"\n"
	if clang {
		config = "CONFIG_CC_IS_CLANG=y\nCONFIG_CLANG_VERSION=190100\nCONFIG_MODULE_SIG=y\nCONFIG_MODULE_SIG_FORCE=y\n"
	}
	writePreflightFile(t, filepath.Join(buildRoot, ".config"), config, 0o644)
	writePreflightFile(t, paths.KernelLockdown, "[none] integrity confidentiality\n", 0o644)
	writePreflightFile(t, paths.OSRelease, "ID=cachyos\nID_LIKE=arch\n", 0o644)
	for _, executable := range []string{
		paths.DKMSExecutables[0], paths.MakeExecutables[0], paths.GCCExecutables[0], paths.LDExecutables[0],
		paths.ClangExecutables[0], paths.LLDExecutables[0], paths.LLVMExecutables[0],
		paths.ModinfoExecutables[0], paths.DepmodExecutables[0], paths.PackageManagers["pacman"][0],
	} {
		writeExecutable(t, executable)
	}
	return paths
}

func writeExecutable(t *testing.T, path string) {
	t.Helper()
	writePreflightFile(t, path, "executable\n", 0o755)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writePreflightFile(t *testing.T, path, contents string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

func checkOK(preflight Preflight, id string) bool {
	for _, check := range preflight.Checks {
		if check.ID == id {
			return check.OK
		}
	}
	return false
}
