package cpuidmodule

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fakePackageRunner struct {
	responses map[string][]byte
}

func TestPackageAdapterRegistryAllowsOnlyMutableFamilies(t *testing.T) {
	registry := DefaultPackageAdapterRegistry()
	tests := []struct {
		name         string
		distribution Distribution
		manager      string
		adapter      string
	}{
		{name: "cachyos", distribution: Distribution{ID: "cachyos"}, manager: "pacman", adapter: "arch-pacman"},
		{name: "arch", distribution: Distribution{ID: "arch"}, manager: "pacman", adapter: "arch-pacman"},
		{name: "mint", distribution: Distribution{ID: "linuxmint"}, manager: "apt", adapter: "debian-apt"},
		{name: "ubuntu", distribution: Distribution{ID: "ubuntu"}, manager: "apt", adapter: "debian-apt"},
		{name: "nobara", distribution: Distribution{ID: "nobara"}, manager: "dnf", adapter: "fedora-dnf"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter, err := registry.Adapter(test.distribution, test.manager)
			if err != nil || adapter.ID() != test.adapter {
				t.Fatalf("adapter = %v, error = %v", adapter, err)
			}
		})
	}
	for _, distribution := range []Distribution{
		{ID: "steamos"}, {ID: "bazzite"}, {ID: "arch", OSTree: true}, {ID: "opensuse"},
	} {
		if _, err := registry.Adapter(distribution, "pacman"); !errors.Is(err, ErrUnsupportedDistribution) {
			t.Fatalf("distribution %+v error = %v", distribution, err)
		}
	}
	if _, err := registry.Adapter(Distribution{ID: "arch"}, "apt"); !errors.Is(err, ErrUnsupportedManager) {
		t.Fatalf("manager error = %v", err)
	}
}

func TestParseDistributionUsesIdentityAndDetectsOstree(t *testing.T) {
	distribution := ParseDistribution([]byte("ID=ubuntu\nID_LIKE=debian\nVARIANT_ID=desktop\n"))
	if distribution.ID != "ubuntu" || distribution.IDLike != "debian" || distribution.OSTree {
		t.Fatalf("distribution = %+v", distribution)
	}
	distribution = ParseDistribution([]byte("ID=bazzite\nVARIANT_ID=gnome-ostree\n"))
	if !distribution.OSTree {
		t.Fatalf("ostree distribution = %+v", distribution)
	}
}

func TestPackageCommandsUseCleanSystemEnvironment(t *testing.T) {
	t.Setenv("LD_LIBRARY_PATH", "/steam/runtime")
	t.Setenv("LD_PRELOAD", "/steam/runtime/lib.so")

	command := execPackageCommand(context.Background(), "/usr/bin/true")
	want := []string{
		"HOME=/root",
		"LANG=C",
		"LC_ALL=C",
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
	}
	if !slices.Equal(command.Env, want) {
		t.Fatalf("command environment = %v, want %v", command.Env, want)
	}
}

func TestPacmanPlanQueriesOwningKernelAndMatchingHeaders(t *testing.T) {
	executable := testPackageExecutable(t)
	runner := fakePackageRunner{responses: map[string][]byte{
		"-Qo /usr/lib/modules/6.18.7-cachyos/vmlinuz":                 []byte("/usr/lib/modules/6.18.7-cachyos/vmlinuz is owned by linux-cachyos/6.18.7-1\n"),
		"-Si linux-cachyos-headers":                                   []byte("Name            : linux-cachyos-headers\n"),
		"-Sp --print-format %n linux-cachyos-headers dkms base-devel": []byte("linux-cachyos-headers\ndkms\nbase-devel\n"),
	}}
	plan, err := DefaultPackageAdapterRegistry().Plan(context.Background(), DependencyPlanRequest{
		Distribution: Distribution{ID: "cachyos"}, Manager: "pacman", Executable: executable,
		KernelRelease: "6.18.7-cachyos", Toolchain: "gcc", Runner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.AdapterID != "arch-pacman" || plan.KernelPackage != "linux-cachyos" ||
		!contains(plan.Packages, "linux-cachyos-headers") || contains(plan.PreviewArgs, "--refresh") {
		t.Fatalf("unexpected Pacman plan: %+v", plan)
	}
	if strings.Contains(strings.Join(plan.InstallArgs, " "), "-u") {
		t.Fatalf("Pacman plan requests an upgrade: %+v", plan)
	}
}

func TestPacmanPlanRejectsUnavailableMatchingHeaders(t *testing.T) {
	executable := testPackageExecutable(t)
	runner := fakePackageRunner{responses: map[string][]byte{
		"-Qo /usr/lib/modules/6.18/vmlinuz": []byte("vmlinuz is owned by linux-cachyos/6.18\n"),
	}}
	_, err := DefaultPackageAdapterRegistry().Plan(context.Background(), DependencyPlanRequest{
		Distribution: Distribution{ID: "arch"}, Manager: "pacman", Executable: executable,
		KernelRelease: "6.18", Toolchain: "gcc", Runner: runner,
	})
	if !errors.Is(err, ErrMissingKernelHeaders) {
		t.Fatalf("error = %v", err)
	}
}

func TestAPTAndDNFPlansUseExactRunningKernelPackages(t *testing.T) {
	executable := testPackageExecutable(t)
	registry := DefaultPackageAdapterRegistry()
	apt, err := registry.Plan(context.Background(), DependencyPlanRequest{
		Distribution: Distribution{ID: "linuxmint"}, Manager: "apt", Executable: executable,
		KernelRelease: "6.8.0-31-generic", Toolchain: "clang",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(apt.Packages, "linux-headers-6.8.0-31-generic") || !contains(apt.Packages, "clang") ||
		contains(apt.Packages, "linux-headers") {
		t.Fatalf("unexpected APT plan: %+v", apt)
	}

	dnf, err := registry.Plan(context.Background(), DependencyPlanRequest{
		Distribution: Distribution{ID: "fedora"}, Manager: "dnf", Executable: executable,
		KernelRelease: "6.10.4-200.fc40.x86_64", Toolchain: "gcc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(dnf.Packages, "kernel-devel-6.10.4-200.fc40.x86_64") || contains(dnf.Packages, "kernel-headers") {
		t.Fatalf("unexpected DNF plan: %+v", dnf)
	}
}

func TestDependencyPlansRejectUnsafeKernelReleases(t *testing.T) {
	executable := testPackageExecutable(t)
	for _, release := range []string{"", "../../other", "6.18 bad"} {
		_, err := DefaultPackageAdapterRegistry().Plan(context.Background(), DependencyPlanRequest{
			Distribution: Distribution{ID: "ubuntu"}, Manager: "apt", Executable: executable,
			KernelRelease: release, Toolchain: "gcc",
		})
		if !errors.Is(err, ErrUnsafeDependencyPlan) {
			t.Fatalf("release %q error = %v", release, err)
		}
	}
}

func TestPackageManagerBusyOutputIsStable(t *testing.T) {
	if !errors.Is(ClassifyPackageManagerOutput("error: failed to acquire the package database lock"), ErrPackageManagerBusy) {
		t.Fatal("lock output was not classified as busy")
	}
	if err := ClassifyPackageManagerOutput("transaction completed"); err != nil {
		t.Fatalf("normal output error = %v", err)
	}
	if err := ValidateTransactionOutput("apt", "The following packages will be upgraded: linux-image-generic", "6.8.0"); !errors.Is(err, ErrUnsafeDependencyPlan) {
		t.Fatalf("kernel upgrade output error = %v", err)
	}
	if err := ValidateTransactionOutput("pacman", ":: Starting full system upgrade...", "6.18"); !errors.Is(err, ErrUnsafeDependencyPlan) {
		t.Fatalf("system upgrade output error = %v", err)
	}
}

func (r fakePackageRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	response, ok := r.responses[key]
	if !ok {
		return nil, errors.New("query failed")
	}
	return response, nil
}

func testPackageExecutable(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "package-manager")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
