package system

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"hv-launcher/internal/model"
)

type fakeRunner struct {
	output []byte
	err    error
	calls  [][]string
}

func (r *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return r.output, r.err
}

func cpuFixture(vendor string, family, modelID int, name, flags string) string {
	return "processor : 0\n" +
		"vendor_id : " + vendor + "\n" +
		"cpu family : " + itoa(family) + "\n" +
		"model : " + itoa(modelID) + "\n" +
		"model name : " + name + "\n" +
		"flags : " + flags + "\n\n"
}

func itoa(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	result := ""
	for value > 0 {
		result = string(digits[value%10]) + result
		value /= 10
	}
	return result
}

func TestCPUClassificationUsesFamilyAndModel(t *testing.T) {
	tests := []struct {
		name         string
		fixture      string
		dmi          string
		architecture string
		supported    bool
		steamDeck    bool
	}{
		{"Intel Ivy Bridge", cpuFixture("GenuineIntel", 6, 0x3a, "Intel CPU", "cpuid_fault"), "", "intel-gen3", false, false},
		{"Intel Haswell", cpuFixture("GenuineIntel", 6, 0x3c, "Intel CPU", "cpuid_fault"), "", "intel-gen4", true, false},
		{"AMD Zen 1", cpuFixture("AuthenticAMD", 23, 0x01, "AMD Ryzen misleading 9000", ""), "", "zen1", true, false},
		{"AMD Zen 2", cpuFixture("AuthenticAMD", 23, 0x31, "AMD Ryzen", ""), "", "zen2", true, false},
		{"AMD Zen 4", cpuFixture("AuthenticAMD", 25, 0x61, "AMD Ryzen", "cpuid_fault"), "", "zen4", true, false},
		{"Steam Deck", cpuFixture("AuthenticAMD", 23, 0x90, "AMD Custom APU 0405", ""), "Valve Jupiter", "zen2", true, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cpu, _, err := classifyCPU(test.fixture, test.dmi)
			if err != nil {
				t.Fatal(err)
			}
			if cpu.Architecture != test.architecture || cpu.Supported != test.supported || cpu.SteamDeck != test.steamDeck {
				t.Fatalf("unexpected classification: %+v", cpu)
			}
		})
	}
}

func TestPathSelectionMatrix(t *testing.T) {
	tests := []struct {
		name     string
		cpu      model.CPUStatus
		kernel   model.KernelStatus
		expected model.PathMode
	}{
		{"Intel native", model.CPUStatus{Vendor: "GenuineIntel", Supported: true}, model.KernelStatus{Major: 6, Minor: 0, Supported: true}, model.PathNative},
		{"Zen 3 hypervisor", model.CPUStatus{Vendor: "AuthenticAMD", Architecture: "zen3", Supported: true}, model.KernelStatus{Major: 6, Minor: 18, Supported: true}, model.PathHypervisor},
		{"Zen 4 old kernel", model.CPUStatus{Vendor: "AuthenticAMD", Architecture: "zen4", Supported: true, CPUIDFaultFlag: true}, model.KernelStatus{Major: 6, Minor: 17, Supported: true}, model.PathHypervisor},
		{"Zen 4 native", model.CPUStatus{Vendor: "AuthenticAMD", Architecture: "zen4", Supported: true, CPUIDFaultFlag: true}, model.KernelStatus{Major: 6, Minor: 18, Supported: true}, model.PathNative},
		{"Zen 4 no flag", model.CPUStatus{Vendor: "AuthenticAMD", Architecture: "zen4", Supported: true}, model.KernelStatus{Major: 6, Minor: 18, Supported: true}, model.PathHypervisor},
		{"Deck", model.CPUStatus{Vendor: "AuthenticAMD", Architecture: "zen2", Supported: true, SteamDeck: true}, model.KernelStatus{Major: 6, Minor: 18, Supported: true}, model.PathHypervisor},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := selectPath(test.cpu, test.kernel); actual != test.expected {
				t.Fatalf("got %s, want %s", actual, test.expected)
			}
		})
	}
}

func TestInspectDerivesActionableStatusesWithoutMutation(t *testing.T) {
	root := t.TempDir()
	write := func(relative, contents string) string {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	cpuPath := write("cpuinfo", cpuFixture("AuthenticAMD", 23, 0x31, "AMD Ryzen", "umip"))
	kernelPath := write("osrelease", "6.10.12-valve\n")
	moduleRoot := filepath.Join(root, "modules")
	if err := os.MkdirAll(filepath.Join(moduleRoot, "kvm_amd"), 0o755); err != nil {
		t.Fatal(err)
	}
	write("modules/kvm_amd/refcnt", "1\n")
	steamRoot := filepath.Join(root, "Steam")
	if err := os.MkdirAll(filepath.Join(steamRoot, "compatibilitytools.d", "GE-Proton11-1-LinUwUx"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{err: errors.New("module missing")}
	inspector := &Inspector{Reader: OSReader{}, Runner: runner, Paths: Paths{
		CPUInfo: cpuPath, KernelRelease: kernelPath, ModulesRoot: moduleRoot, SteamRoots: []string{steamRoot},
	}}
	status, err := inspector.Inspect(context.Background(), "idle")
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != model.StatusSetupRequired || status.Path != model.PathHypervisor {
		t.Fatalf("unexpected status: %+v", status)
	}
	if !status.CPU.UMIPRequiredOff || !status.CPU.UMIPPresent || !status.Modules.KVMBusy || !status.Proton.Found {
		t.Fatalf("missing readiness evidence: %+v", status)
	}
	if len(runner.calls) != 1 || runner.calls[0][0] != "modinfo" {
		t.Fatalf("inspection executed an unexpected command: %v", runner.calls)
	}
	assertCheckRemedy(t, status.Checks, "umip")
	assertCheckRemedy(t, status.Checks, "emulation-module")
}

func TestNativeReadyRequiresAdvertisedCapabilityAndProton(t *testing.T) {
	cpu, flags, err := classifyCPU(cpuFixture("GenuineIntel", 6, 0x3c, "Intel Haswell", "cpuid_fault"), "")
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := classifyKernel("6.18.0")
	if err != nil {
		t.Fatal(err)
	}
	status := deriveStatus(cpu, kernel, selectPath(cpu, kernel), model.ModuleStatus{ControllerState: "idle"}, model.ProtonStatus{Found: true, Tools: []string{"LinUwUx"}}, flags)
	if status.Status != model.StatusNativeReady {
		t.Fatalf("got %s", status.Status)
	}
}

func TestBasicReadinessOutcomes(t *testing.T) {
	supportedKernel := model.KernelStatus{Release: "6.10.0", Major: 6, Minor: 10, Supported: true}
	hypervisorCPU := model.CPUStatus{
		Vendor: "AuthenticAMD", Architecture: "zen3", Generation: "AMD Zen 3",
		Supported: true, UMIPRequiredOff: true,
	}
	linuwux := model.ProtonStatus{Found: true, Tools: []string{"GE-Proton11-1-LinUwUx"}}

	t.Run("hypervisor ready", func(t *testing.T) {
		modules := model.ModuleStatus{
			EmulationInstalled: true, EmulationCompatible: true, ControllerState: "idle",
		}
		status := deriveStatus(hypervisorCPU, supportedKernel, model.PathHypervisor, modules, linuwux, nil)
		if status.Status != model.StatusHypervisorReady {
			t.Fatalf("got %s, want %s", status.Status, model.StatusHypervisorReady)
		}
		assertCheckOK(t, status.Checks, "emulation-module")
		assertCheckOK(t, status.Checks, "proton")
	})

	t.Run("missing Proton requires setup", func(t *testing.T) {
		cpu := model.CPUStatus{
			Vendor: "GenuineIntel", Architecture: "intel-gen4", Generation: "Intel 4th generation",
			Supported: true, CPUIDFaultFlag: true,
		}
		status := deriveStatus(cpu, supportedKernel, model.PathNative, model.ModuleStatus{ControllerState: "idle"}, model.ProtonStatus{}, nil)
		if status.Status != model.StatusSetupRequired {
			t.Fatalf("got %s, want %s", status.Status, model.StatusSetupRequired)
		}
		assertCheckRemedy(t, status.Checks, "proton")
	})

	t.Run("incompatible module requires setup", func(t *testing.T) {
		modules := model.ModuleStatus{
			EmulationInstalled: true, EmulationCompatible: false, ControllerState: "idle",
		}
		status := deriveStatus(hypervisorCPU, supportedKernel, model.PathHypervisor, modules, linuwux, nil)
		if status.Status != model.StatusSetupRequired {
			t.Fatalf("got %s, want %s", status.Status, model.StatusSetupRequired)
		}
		assertCheckRemedy(t, status.Checks, "emulation-module")
	})
}

func TestInspectProtonRejectsUnpatchedSunsetSLR(t *testing.T) {
	steamRoot := t.TempDir()
	toolsRoot := filepath.Join(steamRoot, "compatibilitytools.d")
	for _, name := range []string{"proton-sunset-slr", "GE-Proton11-1-LinUwUx"} {
		if err := os.MkdirAll(filepath.Join(toolsRoot, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	status := (&Inspector{Reader: OSReader{}, Paths: Paths{SteamRoots: []string{steamRoot}}}).inspectProton()
	if !status.Found {
		t.Fatal("expected LinUwUx Proton to be detected")
	}
	if len(status.Tools) != 1 || status.Tools[0] != "GE-Proton11-1-LinUwUx" {
		t.Fatalf("unexpected supported Proton tools: %v", status.Tools)
	}
}

func TestRecoveryStateWinsOverSetupState(t *testing.T) {
	cpu := model.CPUStatus{Vendor: "AuthenticAMD", Architecture: "zen2", Generation: "AMD Zen 2", Supported: true}
	kernel := model.KernelStatus{Release: "6.10", Major: 6, Minor: 10, Supported: true}
	status := deriveStatus(cpu, kernel, model.PathHypervisor, model.ModuleStatus{ControllerState: "recovery-required"}, model.ProtonStatus{}, nil)
	if status.Status != model.StatusRecovery {
		t.Fatalf("got %s", status.Status)
	}
}

func TestUMIPDetailExplainsRequirement(t *testing.T) {
	tests := []struct {
		name     string
		cpu      model.CPUStatus
		expected string
	}{
		{"not required", model.CPUStatus{Generation: "AMD Zen 1"}, "not required"},
		{"disabled as required", model.CPUStatus{Generation: "AMD Zen 3", UMIPRequiredOff: true}, "disabled as required"},
		{"enabled and blocking", model.CPUStatus{Generation: "Intel 9th generation", UMIPRequiredOff: true, UMIPPresent: true}, "enabled and blocking"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := umipDetail(test.cpu); actual != test.expected {
				t.Fatalf("got %q, want %q", actual, test.expected)
			}
		})
	}
}

func assertCheckRemedy(t *testing.T, checks []model.Check, id string) {
	t.Helper()
	for _, check := range checks {
		if check.ID == id {
			if check.OK || check.Remedy == "" {
				t.Fatalf("check %s is not actionable: %+v", id, check)
			}
			return
		}
	}
	t.Fatalf("check %s missing", id)
}

func assertCheckOK(t *testing.T, checks []model.Check, id string) {
	t.Helper()
	for _, check := range checks {
		if check.ID == id {
			if !check.OK || check.Remedy != "" {
				t.Fatalf("check %s is not ready: %+v", id, check)
			}
			return
		}
	}
	t.Fatalf("check %s missing", id)
}
