package system

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"hv-launcher/internal/model"
)

type Inspector struct {
	Reader Reader
	Runner CommandRunner
	Paths  Paths
}

func NewInspector(userHome, runtimeDir string) *Inspector {
	return &Inspector{Reader: OSReader{}, Runner: ExecRunner{}, Paths: DefaultPaths(userHome, runtimeDir)}
}

func (i *Inspector) Inspect(ctx context.Context, controllerState string) (model.SystemStatus, error) {
	cpuData, err := i.Reader.ReadFile(i.Paths.CPUInfo)
	if err != nil {
		return model.SystemStatus{}, fmt.Errorf("read CPU information: %w", err)
	}
	kernelData, err := i.Reader.ReadFile(i.Paths.KernelRelease)
	if err != nil {
		return model.SystemStatus{}, fmt.Errorf("read kernel release: %w", err)
	}
	dmi := strings.Join([]string{
		readOptional(i.Reader, i.Paths.DMIProduct),
		readOptional(i.Reader, i.Paths.DMIBoard),
		readOptional(i.Reader, i.Paths.DMIVendor),
	}, " ")

	cpu, flags, err := classifyCPU(string(cpuData), dmi)
	if err != nil {
		return model.SystemStatus{}, err
	}
	kernel, err := classifyKernel(strings.TrimSpace(string(kernelData)))
	if err != nil {
		return model.SystemStatus{}, err
	}
	path := selectPath(cpu, kernel)
	modules := i.inspectModules(ctx, strings.TrimSpace(string(kernelData)), controllerState)
	proton := i.inspectProton()
	status := deriveStatus(cpu, kernel, path, modules, proton, flags)
	return status, nil
}

func classifyCPU(cpuinfo, dmi string) (model.CPUStatus, map[string]bool, error) {
	fields := map[string]string{}
	for _, line := range strings.Split(cpuinfo, "\n") {
		if strings.TrimSpace(line) == "" && len(fields) > 0 {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if ok {
			fields[strings.TrimSpace(strings.ToLower(key))] = strings.TrimSpace(value)
		}
	}
	vendor := fields["vendor_id"]
	if vendor == "" {
		vendor = fields["vendor"]
	}
	family, familyErr := strconv.Atoi(fields["cpu family"])
	modelID, modelErr := strconv.Atoi(fields["model"])
	if vendor == "" || familyErr != nil || modelErr != nil {
		return model.CPUStatus{}, nil, errors.New("CPU vendor, family, or model is unavailable")
	}
	flags := stringSet(fields["flags"] + " " + fields["features"])
	cpu := model.CPUStatus{
		Vendor:         vendor,
		ModelName:      fields["model name"],
		Family:         family,
		ModelID:        modelID,
		UMIPPresent:    flags["umip"],
		CPUIDFaultFlag: flags["cpuid_fault"],
	}

	switch vendor {
	case "GenuineIntel":
		generation := intelGeneration(family, modelID)
		cpu.Generation = fmt.Sprintf("Intel %dth generation", generation)
		cpu.Architecture = fmt.Sprintf("intel-gen%d", generation)
		cpu.Supported = generation >= 4
		cpu.UMIPRequiredOff = generation >= 9
	case "AuthenticAMD":
		zen := amdZenGeneration(family, modelID)
		cpu.SteamDeck = steamDeckEvidence(family, cpu.ModelName, dmi)
		cpu.Generation = fmt.Sprintf("AMD Zen %d", zen)
		cpu.Architecture = fmt.Sprintf("zen%d", zen)
		cpu.Supported = zen >= 1 || cpu.SteamDeck
		cpu.UMIPRequiredOff = zen >= 2 || cpu.SteamDeck
	default:
		cpu.Generation = "Unknown x86-64 CPU"
		cpu.Architecture = "unknown"
	}
	return cpu, flags, nil
}

func classifyKernel(release string) (model.KernelStatus, error) {
	version := strings.SplitN(release, "-", 2)[0]
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return model.KernelStatus{}, fmt.Errorf("unrecognized kernel release %q", release)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return model.KernelStatus{}, fmt.Errorf("unrecognized kernel release %q", release)
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return model.KernelStatus{}, fmt.Errorf("unrecognized kernel release %q", release)
	}
	return model.KernelStatus{Release: release, Major: major, Minor: minor, Supported: major > 6 || major == 6 && minor >= 0}, nil
}

func selectPath(cpu model.CPUStatus, kernel model.KernelStatus) model.PathMode {
	if !cpu.Supported || !kernel.Supported {
		return model.PathNone
	}
	if cpu.Vendor == "GenuineIntel" {
		return model.PathNative
	}
	if cpu.Vendor != "AuthenticAMD" {
		return model.PathNone
	}
	if cpu.SteamDeck {
		return model.PathHypervisor
	}
	zen := generationNumber(cpu.Architecture)
	if zen >= 4 && kernelAtLeast(kernel, 6, 18) && cpu.CPUIDFaultFlag {
		return model.PathNative
	}
	return model.PathHypervisor
}

func (i *Inspector) inspectModules(ctx context.Context, release, controllerState string) model.ModuleStatus {
	status := model.ModuleStatus{ControllerState: controllerState}
	status.EmulationLoaded = moduleLoaded(i.Reader, i.Paths.ModulesRoot, "cpuid_fault_emulation")
	status.KVMLoaded = moduleLoaded(i.Reader, i.Paths.ModulesRoot, "kvm")
	status.KVMAMDLoaded = moduleLoaded(i.Reader, i.Paths.ModulesRoot, "kvm_amd")
	status.KVMBusy = moduleRefCount(i.Reader, i.Paths.ModulesRoot, "kvm_amd") > 0
	if i.Runner != nil {
		output, err := i.Runner.Run(ctx, "modinfo", "-F", "vermagic", "cpuid_fault_emulation")
		if err == nil {
			status.EmulationInstalled = true
			status.EmulationCompatible = strings.HasPrefix(strings.TrimSpace(string(output)), release+" ") || strings.TrimSpace(string(output)) == release
		}
	}
	return status
}

func (i *Inspector) inspectProton() model.ProtonStatus {
	found := map[string]bool{}
	for _, root := range i.Paths.SteamRoots {
		entries, err := i.Reader.ReadDir(filepath.Join(root, "compatibilitytools.d"))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := entry.Name()
			lower := strings.ToLower(name)
			if entry.IsDir() && strings.Contains(lower, "linuwux") {
				found[name] = true
			}
		}
	}
	tools := make([]string, 0, len(found))
	for name := range found {
		tools = append(tools, name)
	}
	sort.Strings(tools)
	return model.ProtonStatus{Found: len(tools) > 0, Tools: tools}
}

func deriveStatus(cpu model.CPUStatus, kernel model.KernelStatus, path model.PathMode, modules model.ModuleStatus, proton model.ProtonStatus, _ map[string]bool) model.SystemStatus {
	checks := []model.Check{
		{ID: "cpu", OK: cpu.Supported, Label: "CPU", Detail: cpu.Generation, Remedy: failedRemedy(cpu.Supported, "Requires Intel 4th generation or AMD Ryzen 1st generation or newer.")},
		{ID: "kernel", OK: kernel.Supported, Label: "Linux kernel", Detail: kernel.Release, Remedy: failedRemedy(kernel.Supported, "Upgrade to Linux kernel 6.0 or newer.")},
	}
	umipOK := !cpu.UMIPRequiredOff || !cpu.UMIPPresent
	checks = append(checks, model.Check{ID: "umip", OK: umipOK, Label: "UMIP", Detail: umipDetail(cpu), Remedy: failedRemedy(umipOK, "Add clearcpuid=514 (or clearcpuid=umip) to the kernel command line and reboot.")})

	nativeOK := path != model.PathNative || cpu.CPUIDFaultFlag
	if path == model.PathNative {
		checks = append(checks, model.Check{ID: "cpuid-fault", OK: nativeOK, Label: "Native CPUID faulting", Detail: boolDetail(nativeOK, "advertised by the running kernel", "not advertised by the running kernel"), Remedy: failedRemedy(nativeOK, "Use a kernel that exposes the cpuid_fault CPU flag.")})
	}
	if path == model.PathHypervisor {
		checks = append(checks, model.Check{ID: "emulation-module", OK: modules.EmulationInstalled && modules.EmulationCompatible, Label: "CPUID module", Detail: moduleDetail(modules), Remedy: failedRemedy(modules.EmulationInstalled && modules.EmulationCompatible, "Install cpuid_fault_emulation through DKMS for the running kernel; the plugin does not install it.")})
	}
	checks = append(checks, model.Check{ID: "proton", OK: proton.Found, Label: "Proton", Detail: protonDetail(proton), Remedy: failedRemedy(proton.Found, "Manually extract a supported LinUwUx Proton build into Steam's compatibilitytools.d directory.")})

	aggregate := model.StatusSetupRequired
	if modules.ControllerState == "recovery-required" {
		aggregate = model.StatusRecovery
	} else if !cpu.Supported || !kernel.Supported || path == model.PathNone {
		aggregate = model.StatusUnsupported
	} else if allChecksOK(checks) {
		if path == model.PathNative {
			aggregate = model.StatusNativeReady
		} else {
			aggregate = model.StatusHypervisorReady
		}
	}
	return model.SystemStatus{Status: aggregate, Path: path, CPU: cpu, Kernel: kernel, Modules: modules, Proton: proton, Checks: checks}
}

func intelGeneration(family, modelID int) int {
	if family != 6 {
		return 0
	}
	models := map[int]int{
		0x2a: 2, 0x2d: 2, 0x3a: 3, 0x3e: 3,
		0x3c: 4, 0x3f: 4, 0x45: 4, 0x46: 4,
		0x3d: 5, 0x47: 5, 0x4f: 5, 0x56: 5,
		0x4e: 6, 0x5e: 6, 0x8e: 7, 0x9e: 9,
		0x66: 8, 0x7d: 10, 0x7e: 10, 0xa5: 10, 0xa6: 10,
		0x8c: 11, 0x8d: 11, 0x97: 12, 0x9a: 12,
		0xb7: 13, 0xba: 13, 0xaa: 14, 0xac: 14, 0xc5: 15, 0xc6: 15,
	}
	return models[modelID]
}

func amdZenGeneration(family, modelID int) int {
	switch family {
	case 23:
		if modelID >= 0x30 {
			return 2
		}
		return 1
	case 25:
		if modelID >= 0x60 {
			return 4
		}
		return 3
	case 26:
		return 5
	default:
		return 0
	}
}

func steamDeckEvidence(family int, modelName, dmi string) bool {
	if family != 23 {
		return false
	}
	evidence := strings.ToLower(modelName + " " + dmi)
	return strings.Contains(evidence, "steam deck") || strings.Contains(evidence, "jupiter") || strings.Contains(evidence, "galileo") || (strings.Contains(evidence, "amd custom apu") && strings.Contains(evidence, "valve"))
}

func moduleLoaded(reader Reader, root, name string) bool {
	_, err := reader.ReadDir(filepath.Join(root, name))
	return err == nil
}

func moduleRefCount(reader Reader, root, name string) int {
	data, err := reader.ReadFile(filepath.Join(root, name, "refcnt"))
	if err != nil {
		return 0
	}
	value, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return value
}

func readOptional(reader Reader, path string) string {
	data, err := reader.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func stringSet(value string) map[string]bool {
	result := map[string]bool{}
	for _, item := range strings.Fields(value) {
		result[item] = true
	}
	return result
}

func kernelAtLeast(kernel model.KernelStatus, major, minor int) bool {
	return kernel.Major > major || kernel.Major == major && kernel.Minor >= minor
}

func generationNumber(architecture string) int {
	value := strings.TrimPrefix(architecture, "zen")
	number, _ := strconv.Atoi(value)
	return number
}

func failedRemedy(ok bool, remedy string) string {
	if ok {
		return ""
	}
	return remedy
}

func boolDetail(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func umipDetail(cpu model.CPUStatus) string {
	if !cpu.UMIPRequiredOff {
		return "not required"
	}
	if cpu.UMIPPresent {
		return "enabled and blocking"
	}
	return "disabled as required"
}

func moduleDetail(status model.ModuleStatus) string {
	if !status.EmulationInstalled {
		return "not installed for the running kernel"
	}
	if !status.EmulationCompatible {
		return "installed module does not match the running kernel"
	}
	if status.EmulationLoaded {
		return "installed, compatible, and loaded"
	}
	return "installed and compatible"
}

func protonDetail(status model.ProtonStatus) string {
	if !status.Found {
		return "no supported build detected"
	}
	return strings.Join(status.Tools, ", ")
}

func allChecksOK(checks []model.Check) bool {
	for _, check := range checks {
		if !check.OK {
			return false
		}
	}
	return true
}

// Ensure the concrete reader remains compatible with the injected interface.
var _ Reader = OSReader{}
