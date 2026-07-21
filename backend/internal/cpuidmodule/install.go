package cpuidmodule

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type InstallResult struct {
	Inspection      Inspection `json:"inspection"`
	Identity        Identity   `json:"identity"`
	KernelRelease   string     `json:"kernelRelease"`
	ModuleName      string     `json:"moduleName"`
	ModulePath      string     `json:"modulePath"`
	Vermagic        string     `json:"vermagic"`
	Signer          string     `json:"signer,omitempty"`
	NoOp            bool       `json:"noOp"`
	SigningRequired bool       `json:"signingRequired"`
}

type InstallProgress func(phase string, progress int, output string)

type Installer struct {
	Paths  PreflightPaths
	Runner PackageCommandRunner
}

var (
	ErrModuleConflict     = errors.New("CPUID module DKMS registration already exists")
	ErrModuleVerification = errors.New("installed CPUID module could not be verified")
	ErrModuleCleanup      = errors.New("CPUID module registration cleanup failed")
)

const (
	moduleInstallPackage = "cpuid_fault_emulation"
	maxInstallOutput     = 4 << 10
)

func NewInstaller(paths PreflightPaths, runner PackageCommandRunner) *Installer {
	if runner == nil {
		runner = ExecPackageCommandRunner{}
	}
	return &Installer{Paths: paths, Runner: runner}
}

func (i *Installer) Install(ctx context.Context, selectedPath string, dependencyPlan *DependencyPlan, progress InstallProgress) (InstallResult, error) {
	if progress == nil {
		progress = func(string, int, string) {}
	}

	progress("staging-source", 5, "Copying the selected CPUID module archive into private staging")
	inspector := NewInspector()
	stage, err := os.MkdirTemp("", "hv-launcher-cpuid-")
	if err != nil {
		return InstallResult{}, fmt.Errorf("create private module staging: %w", err)
	}
	defer os.RemoveAll(stage)

	stagedArchive := filepath.Join(stage, "source.zip")
	if err := stageSelectedArchive(selectedPath, stagedArchive, inspector.Limits.MaxCompressedBytes); err != nil {
		return InstallResult{}, err
	}

	progress("validating-source", 15, "Validating the staged CPUID module archive")
	inspection, err := inspector.ValidatePath(stagedArchive)
	if err != nil {
		return InstallResult{}, err
	}
	inspection.FileName = filepath.Base(selectedPath)
	if inspection.Identity.PackageName != moduleInstallPackage || inspection.Identity.BuiltModuleName != moduleInstallPackage {
		return InstallResult{}, fmt.Errorf("%w: archive identity is not %s", ErrInvalidDKMSConfig, moduleInstallPackage)
	}

	release, err := runningKernel(i.Paths.KernelRelease)
	if err != nil {
		return InstallResult{}, err
	}
	if dependencyPlan != nil {
		if dependencyPlan.KernelRelease != release {
			return InstallResult{}, fmt.Errorf("%w: dependency plan targets %s, running kernel is %s", ErrUnsafeDependencyPlan, dependencyPlan.KernelRelease, release)
		}
	}

	identity := inspection.Identity
	if compatible, result := i.verifyModule(ctx, release, identity); compatible {
		result.NoOp = true
		result.Inspection = inspection
		result.Identity = identity
		return result, nil
	}

	registration := filepath.Join(i.Paths.DKMSRoot, identity.PackageName, identity.PackageVersion)
	if info, statErr := os.Stat(registration); statErr == nil && info.IsDir() {
		return InstallResult{}, fmt.Errorf("%w: %s/%s is already registered", ErrModuleConflict, identity.PackageName, identity.PackageVersion)
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return InstallResult{}, fmt.Errorf("inspect DKMS registration: %w", statErr)
	}

	source := filepath.Join(stage, "source")
	if err := os.Mkdir(source, 0o700); err != nil {
		return InstallResult{}, fmt.Errorf("create private module source staging: %w", err)
	}
	if err := extractSelectedArchive(stagedArchive, source, inspector.Limits); err != nil {
		return InstallResult{}, err
	}

	if dependencyPlan != nil {
		progress("installing-dependencies", 25, "Installing the reviewed host dependencies")
		if err := i.runDependencyPlan(ctx, *dependencyPlan, progress); err != nil {
			return InstallResult{}, err
		}
	}

	dkms, err := i.fixedExecutable(i.Paths.DKMSExecutables, "DKMS")
	if err != nil {
		return InstallResult{}, err
	}
	identityArg := identity.PackageName + "/" + identity.PackageVersion
	added := false
	cleanup := func(operationErr error) error {
		if !added {
			return operationErr
		}

		progress("cleaning-registration", 85, "Removing the registration created by this setup attempt")
		output, cleanupErr := i.Runner.Run(ctx, dkms, "remove", identityArg, "--all")
		if cleanupErr != nil {
			return errors.Join(operationErr, fmt.Errorf("%w: %v: %s", ErrModuleCleanup, cleanupErr, boundedOutput(output)))
		}
		return operationErr
	}

	progress("registering-source", 40, "Registering the reviewed source with DKMS")
	if output, runErr := i.Runner.Run(ctx, dkms, "add", source); runErr != nil {
		return InstallResult{}, fmt.Errorf("DKMS add failed: %v: %s", runErr, boundedOutput(output))
	}
	added = true

	progress("building-module", 60, "Building the module for the running kernel")
	if output, runErr := i.Runner.Run(ctx, dkms, "build", identityArg, "--force", "-k", release); runErr != nil {
		return InstallResult{}, cleanup(fmt.Errorf("DKMS build failed: %v: %s", runErr, boundedOutput(output)))
	}

	progress("installing-module", 75, "Installing the module for the running kernel")
	if output, runErr := i.Runner.Run(ctx, dkms, "install", identityArg, "--force", "-k", release); runErr != nil {
		return InstallResult{}, cleanup(fmt.Errorf("DKMS install failed: %v: %s", runErr, boundedOutput(output)))
	}

	progress("verifying-module", 92, "Verifying module identity and kernel compatibility")
	verified, result := i.verifyModule(ctx, release, identity)
	if !verified {
		return InstallResult{}, cleanup(fmt.Errorf("%w: modinfo did not report a compatible module", ErrModuleVerification))
	}
	result.Identity = identity
	result.Inspection = inspection
	return result, nil
}

func (i *Installer) runDependencyPlan(ctx context.Context, dependencyPlan DependencyPlan, progress InstallProgress) error {
	output, err := i.Runner.Run(ctx, dependencyPlan.Executable, dependencyPlan.InstallArgs...)
	progress("installing-dependencies", 32, boundedOutput(output))
	if classified := ValidateTransactionOutput(dependencyPlan.Manager, string(output), dependencyPlan.KernelRelease); classified != nil {
		return classified
	}
	if err != nil {
		return fmt.Errorf("package transaction failed: %v: %s", err, boundedOutput(output))
	}

	return nil
}

func stageSelectedArchive(selectedPath, stagedPath string, maximum int64) error {
	input, _, err := openSelectedFile(selectedPath)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.OpenFile(stagedPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create staged module archive: %w", err)
	}

	written, copyErr := io.Copy(output, io.LimitReader(input, maximum+1))
	closeErr := output.Close()
	if copyErr != nil {
		return fmt.Errorf("copy selected module archive: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close staged module archive: %w", closeErr)
	}
	if written > maximum {
		return fmt.Errorf("%w: compressed archive exceeds %d bytes", ErrResourceLimit, maximum)
	}

	return nil
}

func (i *Installer) verifyModule(ctx context.Context, release string, identity Identity) (bool, InstallResult) {
	modinfo, err := i.fixedExecutable(i.Paths.ModinfoExecutables, "modinfo")
	if err != nil {
		return false, InstallResult{}
	}

	name := identity.BuiltModuleName
	moduleName, nameErr := i.Runner.Run(ctx, modinfo, "-k", release, "-F", "name", name)
	if nameErr != nil || strings.TrimSpace(string(moduleName)) != name {
		return false, InstallResult{}
	}

	pathOutput, pathErr := i.Runner.Run(ctx, modinfo, "-k", release, "-F", "filename", name)
	modulePath := strings.TrimSpace(string(pathOutput))
	if pathErr != nil || !validModulePath(modulePath, release, name) {
		return false, InstallResult{}
	}

	vermagic, magicErr := i.Runner.Run(ctx, modinfo, "-k", release, "-F", "vermagic", name)
	if magicErr != nil || !strings.HasPrefix(strings.TrimSpace(string(vermagic)), release) {
		return false, InstallResult{}
	}

	signer, _ := i.Runner.Run(ctx, modinfo, "-k", release, "-F", "signer", name)
	lockdown := readLockdown(i.Paths.KernelLockdown)
	result := InstallResult{
		KernelRelease: release,
		ModuleName:    name,
		ModulePath:    boundedOutput([]byte(modulePath)),
		Vermagic:      boundedOutput(vermagic),
		Signer:        boundedOutput(signer),
	}
	result.SigningRequired = (lockdown == "integrity" || lockdown == "confidentiality") && result.Signer == ""
	return true, result
}

func validModulePath(modulePath, release, moduleName string) bool {
	if modulePath == "" || !filepath.IsAbs(modulePath) || strings.ContainsRune(modulePath, '\x00') {
		return false
	}
	clean := filepath.Clean(modulePath)
	if clean != modulePath || !strings.Contains(clean, string(filepath.Separator)+release+string(filepath.Separator)) {
		return false
	}

	base := filepath.Base(clean)
	return base == moduleName+".ko" || base == moduleName+".ko.xz" || base == moduleName+".ko.zst" || base == moduleName+".ko.gz"
}

func (i *Installer) fixedExecutable(candidates []string, label string) (string, error) {
	executable := firstExecutable(candidates)
	if executable == "" {
		return "", fmt.Errorf("%s is unavailable", label)
	}
	return executable, nil
}

func runningKernel(path string) (string, error) {
	data, err := readBoundedFile(path, 4<<10)
	if err != nil {
		return "", fmt.Errorf("read running kernel release: %w", err)
	}
	release := strings.TrimSpace(string(data))
	if !validKernelRelease(release) {
		return "", fmt.Errorf("%w: running kernel release is invalid", ErrUnsafeDependencyPlan)
	}

	return release, nil
}

func extractSelectedArchive(selectedPath, destination string, limits Limits) error {
	file, size, err := openSelectedFile(selectedPath)
	if err != nil {
		return err
	}
	defer file.Close()

	archive, err := zip.NewReader(file, size)
	if err != nil {
		return fmt.Errorf("%w: open ZIP for extraction: %v", ErrInvalidArchive, err)
	}
	for _, entry := range archive.File {
		name, directory, err := normalizeZIPPath(entry, limits)
		if err != nil {
			return err
		}
		entryPath := filepath.Join(destination, filepath.FromSlash(name))
		if directory {
			if err := os.MkdirAll(entryPath, 0o755); err != nil {
				return fmt.Errorf("create staged directory %q: %w", name, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(entryPath), 0o755); err != nil {
			return fmt.Errorf("create staged parent for %q: %w", name, err)
		}
		mode := entry.Mode().Perm() & 0o777
		if mode == 0 {
			mode = 0o644
		}

		output, err := os.OpenFile(entryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if err != nil {
			return fmt.Errorf("create staged file %q: %w", name, err)
		}
		reader, openErr := entry.Open()
		var written int64
		if openErr == nil {
			written, openErr = io.Copy(output, io.LimitReader(reader, limits.MaxFileBytes+1))
			closeErr := reader.Close()
			if openErr == nil {
				openErr = closeErr
			}
		}

		closeErr := output.Close()
		if openErr == nil {
			openErr = closeErr
		}

		if openErr != nil {
			return fmt.Errorf("extract staged file %q: %w", name, openErr)
		}
		if written > limits.MaxFileBytes {
			return fmt.Errorf("%w: %q exceeds %d bytes", ErrResourceLimit, name, limits.MaxFileBytes)
		}
	}
	return nil
}

func boundedOutput(output []byte) string {
	value := strings.TrimSpace(string(output))
	if len(value) > maxInstallOutput {
		value = value[:maxInstallOutput]
	}
	return value
}
