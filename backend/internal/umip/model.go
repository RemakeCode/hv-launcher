package umip

import "errors"

const FixedArgument = "clearcpuid=514"

var (
	ErrUnsupportedBootloader = errors.New("unsupported bootloader")
	ErrCandidateUnavailable  = errors.New("bootloader candidate is unavailable")
	ErrNoChangeRequired      = errors.New("UMIP boot configuration does not need changing")
	ErrConfigurationChanged  = errors.New("UMIP configuration changed during update")
)

type Bootloader string

const (
	BootloaderLimine Bootloader = "limine"
	BootloaderGRUB   Bootloader = "grub"
)

type SelectionMode string

const (
	SelectionAutomatic  SelectionMode = "automatic"
	SelectionChoice     SelectionMode = "choice-required"
	SelectionManualOnly SelectionMode = "manual-only"
)

type CandidateState string

const (
	StateActionRequired  CandidateState = "action-required"
	StateRestartRequired CandidateState = "restart-required"
	StateConfigured      CandidateState = "configured"
)

type ManualReason string

const (
	ReasonUnsupportedSyntax   ManualReason = "unsupported-syntax"
	ReasonMissingUpdater      ManualReason = "missing-updater"
	ReasonConflictingArgument ManualReason = "conflicting-argument"
	ReasonUnsupportedLoader   ManualReason = "unsupported-bootloader"
)

type Updater struct {
	Path string   `json:"path"`
	Args []string `json:"args"`
}

type Candidate struct {
	Bootloader       Bootloader     `json:"bootloader"`
	Configuration    string         `json:"configuration"`
	Updater          Updater        `json:"updater"`
	State            CandidateState `json:"state"`
	CurrentValue     string         `json:"currentValue"`
	ProposedValue    string         `json:"proposedValue"`
	ExistingArgument string         `json:"existingArgument,omitempty"`
	Detail           string         `json:"detail"`
}

type ManualOutcome struct {
	Bootloader Bootloader   `json:"bootloader,omitempty"`
	Reason     ManualReason `json:"reason"`
	Detail     string       `json:"detail"`
}

type Inspection struct {
	LiveUMIP   bool            `json:"liveUmip"`
	Selection  SelectionMode   `json:"selection"`
	Selected   Bootloader      `json:"selected,omitempty"`
	Candidates []Candidate     `json:"candidates"`
	Manual     []ManualOutcome `json:"manual"`
}

type ApplyResult struct {
	Bootloader      Bootloader `json:"bootloader"`
	RestartRequired bool       `json:"restartRequired"`
	BackupRetained  string     `json:"backupRetained,omitempty"`
}

type ProgressFunc func(phase string, progress int, message string)

func UMIPApplyBinding(bootloader Bootloader) string {
	return "umip-apply:" + string(bootloader)
}
