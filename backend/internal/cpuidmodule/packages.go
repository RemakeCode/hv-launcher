package cpuidmodule

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// PackageCommandRunner is deliberately local to the package-adapter layer.
// It makes package-manager queries testable without sharing command plumbing
// with unrelated host inspections.
type PackageCommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type ExecPackageCommandRunner struct{}

// Distribution is the small, parsed subset of /etc/os-release used for
// adapter selection. ID_LIKE is informational and never selects an adapter by
// itself.
type Distribution struct {
	ID        string
	IDLike    string
	VariantID string
	OSTree    bool
}

// DependencyPlan is the complete backend-derived package transaction shown
// for user confirmation. Callers cannot add package names or manager arguments.
type DependencyPlan struct {
	AdapterID     string   `json:"-"`
	Manager       string   `json:"manager"`
	Executable    string   `json:"-"`
	KernelRelease string   `json:"-"`
	KernelPackage string   `json:"-"`
	Toolchain     string   `json:"-"`
	Packages      []string `json:"packages"`
	PreviewArgs   []string `json:"-"`
	PreviewOutput string   `json:"previewOutput,omitempty"`
	InstallArgs   []string `json:"-"`
}

type DependencyPlanRequest struct {
	Distribution    Distribution
	Manager         string
	Executable      string
	KernelRelease   string
	Toolchain       string
	KernelPackage   string
	KernelOwnerPath string
	Runner          PackageCommandRunner
}

type PackageAdapter interface {
	ID() string
	Matches(Distribution, string) bool
	Plan(context.Context, DependencyPlanRequest) (DependencyPlan, error)
}

type PackageAdapterRegistry struct {
	adapters []PackageAdapter
}

type pacmanAdapter struct{}

type aptAdapter struct{}

type dnfAdapter struct{}

var (
	ErrUnsupportedDistribution = errors.New("unsupported or immutable distribution")
	ErrUnsupportedManager      = errors.New("unsupported package manager")
	ErrPackageManagerBusy      = errors.New("package manager is busy")
	ErrUnsafeDependencyPlan    = errors.New("unsafe dependency plan")
	ErrMissingKernelHeaders    = errors.New("matching kernel headers are unavailable")
)

const maxPackagePreviewBytes = 16 << 10

var (
	packageNamePattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9+_.:@~-]{0,127}$`)
	kernelReleasePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9+_.:@~-]{0,255}$`)
	ownedPackagePattern  = regexp.MustCompile(`\bis owned by ([A-Za-z0-9][A-Za-z0-9+_.:@~-]*)/`)
)

func (ExecPackageCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return execPackageCommand(ctx, name, args...).CombinedOutput()
}

func execPackageCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = []string{
		"HOME=/root",
		"LANG=C",
		"LC_ALL=C",
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
	}

	return command
}

// ParseDistribution parses static os-release assignments without evaluating
// shell syntax. Unknown fields are ignored.
func ParseDistribution(data []byte) Distribution {
	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		equals := strings.IndexByte(line, '=')
		if equals <= 0 {
			continue
		}
		name := line[:equals]
		if name != "ID" && name != "ID_LIKE" && name != "VARIANT_ID" && name != "OSTREE_VERSION" {
			continue
		}
		value := strings.TrimSpace(line[equals+1:])
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		values[name] = strings.ToLower(value)
	}

	distribution := Distribution{
		ID:        values["ID"],
		IDLike:    values["ID_LIKE"],
		VariantID: values["VARIANT_ID"],
		OSTree:    values["OSTREE_VERSION"] != "",
	}
	lower := strings.ToLower(distribution.ID + " " + distribution.VariantID + " " + distribution.IDLike)
	distribution.OSTree = distribution.OSTree || strings.Contains(lower, "ostree") ||
		strings.Contains(lower, "atomic")
	return distribution
}

func (p DependencyPlan) Validate() error {
	if p.AdapterID == "" || p.Manager == "" || p.Executable == "" || !filepath.IsAbs(p.Executable) {
		return fmt.Errorf("%w: manager identity or executable is invalid", ErrUnsafeDependencyPlan)
	}
	if !validKernelRelease(p.KernelRelease) || p.Toolchain == "" || len(p.Packages) == 0 {
		return fmt.Errorf("%w: kernel or package set is invalid", ErrUnsafeDependencyPlan)
	}
	for _, packageName := range p.Packages {
		if !packageNamePattern.MatchString(packageName) {
			return fmt.Errorf("%w: package %q is invalid", ErrUnsafeDependencyPlan, packageName)
		}
	}
	if err := validateFixedArguments(p.Manager, p.PreviewArgs, true); err != nil {
		return err
	}
	if err := validateFixedArguments(p.Manager, p.InstallArgs, false); err != nil {
		return err
	}
	return nil
}

func DefaultPackageAdapterRegistry() PackageAdapterRegistry {
	return PackageAdapterRegistry{adapters: []PackageAdapter{
		pacmanAdapter{}, aptAdapter{}, dnfAdapter{},
	}}
}

func (r PackageAdapterRegistry) Adapter(distribution Distribution, manager string) (PackageAdapter, error) {
	if distribution.OSTree || !supportedDistributionID(distribution.ID) {
		return nil, ErrUnsupportedDistribution
	}
	for _, adapter := range r.adapters {
		if adapter.Matches(distribution, manager) {
			return adapter, nil
		}
	}
	return nil, ErrUnsupportedManager
}

func (r PackageAdapterRegistry) Plan(ctx context.Context, request DependencyPlanRequest) (DependencyPlan, error) {
	adapter, err := r.Adapter(request.Distribution, request.Manager)
	if err != nil {
		return DependencyPlan{}, err
	}
	return adapter.Plan(ctx, request)
}

func supportedDistributionID(id string) bool {
	switch strings.ToLower(id) {
	case "arch", "cachyos", "debian", "ubuntu", "linuxmint", "mint", "fedora", "nobara":
		return true
	default:
		return false
	}
}

func (pacmanAdapter) ID() string { return "arch-pacman" }

func (pacmanAdapter) Matches(distribution Distribution, manager string) bool {
	return (strings.EqualFold(distribution.ID, "arch") || strings.EqualFold(distribution.ID, "cachyos")) && manager == "pacman"
}

func (a pacmanAdapter) Plan(ctx context.Context, request DependencyPlanRequest) (DependencyPlan, error) {
	executable := request.Executable
	if request.KernelPackage == "" {
		if request.Runner == nil {
			return DependencyPlan{}, fmt.Errorf("%w: Pacman kernel ownership query is unavailable", ErrMissingKernelHeaders)
		}
		ownerPath := request.KernelOwnerPath
		if ownerPath == "" {
			if !validKernelRelease(request.KernelRelease) {
				return DependencyPlan{}, fmt.Errorf("%w: running kernel release is invalid", ErrUnsafeDependencyPlan)
			}
			ownerPath = filepath.Join("/usr/lib/modules", request.KernelRelease, "vmlinuz")
		}
		output, runErr := request.Runner.Run(ctx, executable, "-Qo", ownerPath)
		if runErr != nil {
			return DependencyPlan{}, fmt.Errorf("%w: query kernel package: %v", ErrMissingKernelHeaders, runErr)
		}
		matches := ownedPackagePattern.FindStringSubmatch(string(output))
		if len(matches) != 2 {
			return DependencyPlan{}, fmt.Errorf("%w: Pacman did not identify the running kernel package", ErrMissingKernelHeaders)
		}
		request.KernelPackage = matches[1]
	}
	if !packageNamePattern.MatchString(request.KernelPackage) || strings.HasSuffix(request.KernelPackage, "-headers") {
		return DependencyPlan{}, fmt.Errorf("%w: kernel package identity is invalid", ErrUnsafeDependencyPlan)
	}
	headerPackage := request.KernelPackage + "-headers"
	if request.Runner != nil {
		output, runErr := request.Runner.Run(ctx, executable, "-Si", headerPackage)
		if runErr != nil || !strings.Contains(strings.ToLower(string(output)), strings.ToLower(headerPackage)) {
			return DependencyPlan{}, fmt.Errorf("%w: matching package %q is unavailable", ErrMissingKernelHeaders, headerPackage)
		}
	}
	packages := []string{headerPackage, "dkms", "base-devel"}
	if request.Toolchain == "clang" {
		packages = append(packages, "clang", "lld", "llvm")
	}
	plan, err := newDependencyPlan(a.ID(), "pacman", executable, request, request.KernelPackage, packages,
		[]string{"-Sp", "--print-format", "%n"}, []string{"--needed", "-S", "--noconfirm"})
	if err != nil {
		return DependencyPlan{}, err
	}
	return resolvePackagePreview(ctx, plan, request.Runner)
}

func (aptAdapter) ID() string { return "debian-apt" }

func (aptAdapter) Matches(distribution Distribution, manager string) bool {
	switch strings.ToLower(distribution.ID) {
	case "debian", "ubuntu", "linuxmint", "mint":
		return manager == "apt"
	default:
		return false
	}
}

func (a aptAdapter) Plan(ctx context.Context, request DependencyPlanRequest) (DependencyPlan, error) {
	executable := request.Executable
	if !validKernelRelease(request.KernelRelease) {
		return DependencyPlan{}, fmt.Errorf("%w: running kernel release is invalid", ErrUnsafeDependencyPlan)
	}
	packages := []string{"linux-headers-" + request.KernelRelease, "dkms", "build-essential"}
	if request.Toolchain == "clang" {
		packages = append(packages, "clang", "lld", "llvm")
	}
	plan, err := newDependencyPlan(a.ID(), "apt", executable, request, "", packages,
		[]string{"--simulate", "install"}, []string{"install", "--yes", "--no-install-recommends"})
	if err != nil {
		return DependencyPlan{}, err
	}
	return resolvePackagePreview(ctx, plan, request.Runner)
}

func (dnfAdapter) ID() string { return "fedora-dnf" }

func (dnfAdapter) Matches(distribution Distribution, manager string) bool {
	switch strings.ToLower(distribution.ID) {
	case "fedora", "nobara":
		return manager == "dnf"
	default:
		return false
	}
}

func (a dnfAdapter) Plan(ctx context.Context, request DependencyPlanRequest) (DependencyPlan, error) {
	executable := request.Executable
	if !validKernelRelease(request.KernelRelease) {
		return DependencyPlan{}, fmt.Errorf("%w: running kernel release is invalid", ErrUnsafeDependencyPlan)
	}
	packages := []string{"kernel-devel-" + request.KernelRelease, "dkms", "gcc", "make", "binutils"}
	if request.Toolchain == "clang" {
		packages = append(packages, "clang", "lld", "llvm")
	}
	plan, err := newDependencyPlan(a.ID(), "dnf", executable, request, "", packages,
		[]string{"install", "--assumeno"}, []string{"install", "--assumeyes"})
	if err != nil {
		return DependencyPlan{}, err
	}
	return resolvePackagePreview(ctx, plan, request.Runner)
}

func resolvePackagePreview(ctx context.Context, plan DependencyPlan, runner PackageCommandRunner) (DependencyPlan, error) {
	if runner == nil {
		return plan, nil
	}
	output, err := runner.Run(ctx, plan.Executable, plan.PreviewArgs...)
	if classified := ValidateTransactionOutput(plan.Manager, string(output), plan.KernelRelease); classified != nil {
		return DependencyPlan{}, classified
	}
	if err != nil {
		return DependencyPlan{}, fmt.Errorf("package preview failed: %v: %s", err, boundedPackagePreview(output))
	}
	plan.PreviewOutput = boundedPackagePreview(output)
	return plan, nil
}

func boundedPackagePreview(output []byte) string {
	value := strings.TrimSpace(string(output))
	if len(value) > maxPackagePreviewBytes {
		value = value[:maxPackagePreviewBytes]
	}
	return value
}

func newDependencyPlan(adapterID, manager, executable string, request DependencyPlanRequest, kernelPackage string, packages, preview, install []string) (DependencyPlan, error) {
	plan := DependencyPlan{
		AdapterID: adapterID, Manager: manager, Executable: executable,
		KernelRelease: request.KernelRelease, KernelPackage: kernelPackage,
		Toolchain: request.Toolchain, Packages: append([]string(nil), packages...),
		PreviewArgs: append(append([]string(nil), preview...), packages...),
		InstallArgs: append(append([]string(nil), install...), packages...),
	}
	if err := plan.Validate(); err != nil {
		return DependencyPlan{}, err
	}
	return plan, nil
}

func validateFixedArguments(manager string, arguments []string, preview bool) error {
	if len(arguments) == 0 {
		return fmt.Errorf("%w: package manager arguments are empty", ErrUnsafeDependencyPlan)
	}
	joined := " " + strings.Join(arguments, " ") + " "
	for _, forbidden := range []string{" -u ", " --sysupgrade ", " full-upgrade ", " dist-upgrade ", " remove ", " purge ", " --refresh ", " -y ", " --yes-all "} {
		if strings.Contains(joined, forbidden) {
			return fmt.Errorf("%w: dependency plan contains forbidden argument %q", ErrUnsafeDependencyPlan, strings.TrimSpace(forbidden))
		}
	}
	if manager == "pacman" && preview && strings.Contains(joined, " -S ") {
		return fmt.Errorf("%w: Pacman preview must not install", ErrUnsafeDependencyPlan)
	}
	return nil
}

func validKernelRelease(release string) bool {
	return release != "" && kernelReleasePattern.MatchString(release) && !strings.Contains(release, "..")
}

// ClassifyPackageManagerOutput turns common lock failures into a stable error
// without attempting to remove a lock or retry a package transaction.
func ClassifyPackageManagerOutput(output string) error {
	lower := strings.ToLower(output)
	if strings.Contains(lower, "unable to lock") || strings.Contains(lower, "could not get lock") ||
		strings.Contains(lower, "another process") || strings.Contains(lower, "database is locked") ||
		strings.Contains(lower, "database lock") || strings.Contains(lower, "acquire the package") {
		return ErrPackageManagerBusy
	}
	return nil
}

// ValidateTransactionOutput rejects a resolved transaction that broadens
// beyond the reviewed dependency set. Package-manager output is advisory and
// never replaces the fixed argument validation performed before execution.
func ValidateTransactionOutput(manager, output, kernelRelease string) error {
	if err := ClassifyPackageManagerOutput(output); err != nil {
		return err
	}
	lower := strings.ToLower(output)
	for _, forbidden := range []string{"full system upgrade", "dist-upgrade", "full-upgrade", "sysupgrade"} {
		if strings.Contains(lower, forbidden) {
			return fmt.Errorf("%w: transaction requests %s", ErrUnsafeDependencyPlan, forbidden)
		}
	}
	if (strings.Contains(lower, "kernel") || strings.Contains(lower, "linux-image")) && (strings.Contains(lower, " will be upgraded") ||
		strings.Contains(lower, " upgrading") || strings.Contains(lower, " update")) {
		return fmt.Errorf("%w: transaction would change a kernel", ErrUnsafeDependencyPlan)
	}
	if manager == "pacman" && strings.Contains(lower, ":: starting full system upgrade") {
		return fmt.Errorf("%w: Pacman requested a system upgrade", ErrUnsafeDependencyPlan)
	}
	if kernelRelease != "" && strings.Contains(lower, "linux-image-"+strings.ToLower(kernelRelease)) && strings.Contains(lower, "upgrade") {
		return fmt.Errorf("%w: transaction would replace the running kernel", ErrUnsafeDependencyPlan)
	}
	return nil
}
