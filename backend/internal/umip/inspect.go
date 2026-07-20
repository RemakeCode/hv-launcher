package umip

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const MaxConfigurationBytes int64 = 1 << 20

type Paths struct {
	LimineConfiguration string
	GRUBConfiguration   string
	GRUBOutput          string
	SystemdEntries      string
	LimineUpdaters      []string
	UpdateGRUB          []string
	GRUBMkconfig        []string
	RecoveryDirectory   string
}

func DefaultPaths() Paths {
	return Paths{
		LimineConfiguration: "/etc/default/limine",
		GRUBConfiguration:   "/etc/default/grub",
		GRUBOutput:          "/boot/grub/grub.cfg",
		SystemdEntries:      "/boot/loader/entries",
		LimineUpdaters:      []string{"/usr/bin/limine-update", "/usr/local/bin/limine-update"},
		UpdateGRUB:          []string{"/usr/sbin/update-grub", "/usr/bin/update-grub"},
		GRUBMkconfig:        []string{"/usr/bin/grub-mkconfig", "/usr/sbin/grub-mkconfig"},
		RecoveryDirectory:   "/var/lib/hv-launcher/recovery",
	}
}

type Inspector struct {
	Paths  Paths
	Runner CommandRunner
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

func NewInspector(paths Paths) *Inspector {
	return &Inspector{Paths: paths, Runner: safeExecRunner{}}
}

func (i *Inspector) Inspect(liveUMIP bool) Inspection {
	result := Inspection{
		LiveUMIP:   liveUMIP,
		Candidates: make([]Candidate, 0, 2),
		Manual:     make([]ManualOutcome, 0, 2),
	}
	i.inspectLimine(&result)
	i.inspectGRUB(&result)
	if len(result.Candidates) == 0 && len(result.Manual) == 0 && directoryExists(i.Paths.SystemdEntries) {
		result.Manual = append(result.Manual, ManualOutcome{
			Reason: ReasonUnsupportedLoader,
			Detail: "systemd-boot is not supported; add clearcpuid=514 manually.",
		})
	}
	if len(result.Candidates) == 0 && len(result.Manual) == 0 {
		result.Manual = append(result.Manual, ManualOutcome{
			Reason: ReasonUnsupportedLoader,
			Detail: "No supported Limine or GRUB configuration was found; add clearcpuid=514 manually.",
		})
	}
	switch len(result.Candidates) {
	case 0:
		result.Selection = SelectionManualOnly
	case 1:
		result.Selection = SelectionAutomatic
		result.Selected = result.Candidates[0].Bootloader
	default:
		result.Selection = SelectionChoice
	}
	return result
}

func (i *Inspector) inspectLimine(result *Inspection) {
	data, present, err := readConfiguration(i.Paths.LimineConfiguration)
	if !present {
		return
	}
	if err != nil {
		result.Manual = append(result.Manual, manual(BootloaderLimine, ReasonUnsupportedSyntax, err))
		return
	}
	values, err := parseLimine(data)
	if err != nil {
		result.Manual = append(result.Manual, manual(BootloaderLimine, ReasonUnsupportedSyntax, err))
		return
	}
	state, token := inspectArguments(values)
	if state == "" {
		result.Manual = append(result.Manual, ManualOutcome{
			Bootloader: BootloaderLimine,
			Reason:     ReasonConflictingArgument,
			Detail:     fmt.Sprintf("Limine contains conflicting kernel argument %q; resolve it manually.", token),
		})
		return
	}
	updater, err := i.limineUpdater()
	if err != nil {
		result.Manual = append(result.Manual, ManualOutcome{
			Bootloader: BootloaderLimine,
			Reason:     ReasonMissingUpdater,
			Detail:     err.Error(),
		})
		return
	}
	result.Candidates = append(result.Candidates, candidate(
		BootloaderLimine, i.Paths.LimineConfiguration, updater, values, state, token, result.LiveUMIP,
	))
}

func (i *Inspector) inspectGRUB(result *Inspection) {
	data, present, err := readConfiguration(i.Paths.GRUBConfiguration)
	if !present {
		return
	}
	if err != nil {
		result.Manual = append(result.Manual, manual(BootloaderGRUB, ReasonUnsupportedSyntax, err))
		return
	}
	values, err := parseGRUB(data)
	if err != nil {
		result.Manual = append(result.Manual, manual(BootloaderGRUB, ReasonUnsupportedSyntax, err))
		return
	}
	state, token := inspectArguments(values)
	if state == "" {
		result.Manual = append(result.Manual, ManualOutcome{
			Bootloader: BootloaderGRUB,
			Reason:     ReasonConflictingArgument,
			Detail:     fmt.Sprintf("GRUB contains conflicting kernel argument %q; resolve it manually.", token),
		})
		return
	}
	updater, err := i.grubUpdater()
	if err != nil {
		result.Manual = append(result.Manual, ManualOutcome{
			Bootloader: BootloaderGRUB,
			Reason:     ReasonMissingUpdater,
			Detail:     err.Error(),
		})
		return
	}
	result.Candidates = append(result.Candidates, candidate(
		BootloaderGRUB, i.Paths.GRUBConfiguration, updater, values, state, token, result.LiveUMIP,
	))
}

func (i *Inspector) limineUpdater() (Updater, error) {
	if path := firstExecutable(i.Paths.LimineUpdaters); path != "" {
		return Updater{Path: path, Args: []string{}}, nil
	}
	return Updater{}, fmt.Errorf("%w: allowlisted limine-update executable is unavailable", ErrCandidateUnavailable)
}

func (i *Inspector) grubUpdater() (Updater, error) {
	if path := firstExecutable(i.Paths.UpdateGRUB); path != "" {
		return Updater{Path: path, Args: []string{}}, nil
	}
	if path := firstExecutable(i.Paths.GRUBMkconfig); path != "" && regularFile(i.Paths.GRUBOutput) {
		return Updater{Path: path, Args: []string{"-o", i.Paths.GRUBOutput}}, nil
	}
	return Updater{}, fmt.Errorf("%w: neither update-grub nor grub-mkconfig with the expected output is available", ErrCandidateUnavailable)
}

func candidate(bootloader Bootloader, configuration string, updater Updater, values []string, state CandidateState, token string, liveUMIP bool) Candidate {
	current := strings.TrimSpace(strings.Join(values, " "))
	if bootloader == BootloaderGRUB && len(values) > 0 {
		current = values[0]
	}
	proposed := current
	detail := FixedArgument + " can be added after review."
	if state == StateConfigured {
		if liveUMIP {
			state = StateRestartRequired
			detail = "The UMIP argument is already configured; restart the system to apply it."
		} else {
			detail = "The UMIP argument is already configured."
		}
	} else {
		proposed = appendArgument(current)
	}
	return Candidate{
		Bootloader: bootloader, Configuration: configuration, Updater: updater,
		State: state, CurrentValue: current, ProposedValue: proposed,
		ExistingArgument: token, Detail: detail,
	}
}

func appendArgument(current string) string {
	if current == "" {
		return FixedArgument
	}
	if last := current[len(current)-1]; last == ' ' || last == '\t' {
		return current + FixedArgument
	}
	return current + " " + FixedArgument
}

func manual(bootloader Bootloader, reason ManualReason, err error) ManualOutcome {
	return ManualOutcome{Bootloader: bootloader, Reason: reason, Detail: err.Error()}
}

func readConfiguration(path string) ([]byte, bool, error) {
	snapshot, err := readConfigurationSnapshot(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, true, err
	}
	return snapshot.data, true, nil
}

func firstExecutable(paths []string) string {
	for _, path := range paths {
		if executable, err := exec.LookPath(path); err == nil {
			return executable
		}
	}
	return ""
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
