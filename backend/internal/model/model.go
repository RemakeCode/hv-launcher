package model

import "hv-launcher/internal/linuwux"

type AggregateStatus string

const (
	StatusNativeReady     AggregateStatus = "native-ready"
	StatusHypervisorReady AggregateStatus = "hypervisor-ready"
	StatusSetupRequired   AggregateStatus = "setup-required"
	StatusRecovery        AggregateStatus = "recovery-required"
	StatusUnsupported     AggregateStatus = "unsupported"
)

type ModuleVerificationState string

const (
	ModuleVerificationPending  ModuleVerificationState = "pending"
	ModuleVerificationVerified ModuleVerificationState = "verified"
	ModuleVerificationFailed   ModuleVerificationState = "failed"
)

type ModuleVerificationClassification string

const (
	ModuleVerificationGenericFailure ModuleVerificationClassification = "activation-failure"
	ModuleVerificationKeyRejected    ModuleVerificationClassification = "signature-key-rejection"
	ModuleVerificationBlocked        ModuleVerificationClassification = "blocked"
)

type ModuleVerificationOutcome struct {
	State          ModuleVerificationState          `json:"state"`
	Classification ModuleVerificationClassification `json:"classification,omitempty"`
	Detail         string                           `json:"detail,omitempty"`
	Remediation    string                           `json:"remediation,omitempty"`
}

type ModuleVerificationStore interface {
	Load() (ModuleVerificationOutcome, error)
	SaveIfChanged(ModuleVerificationOutcome) error
}

type PathMode string

const (
	PathNative     PathMode = "native"
	PathHypervisor PathMode = "hypervisor"
	PathNone       PathMode = "none"
)

type Check struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
	Remedy string `json:"remedy,omitempty"`
}

type CPUStatus struct {
	Vendor          string `json:"vendor"`
	ModelName       string `json:"modelName"`
	Family          int    `json:"family"`
	ModelID         int    `json:"modelId"`
	Architecture    string `json:"architecture"`
	Generation      string `json:"generation"`
	Supported       bool   `json:"supported"`
	SteamDeck       bool   `json:"steamDeck"`
	UMIPPresent     bool   `json:"umipPresent"`
	UMIPRequiredOff bool   `json:"umipRequiredOff"`
	CPUIDFaultFlag  bool   `json:"cpuidFaultFlag"`
}

type KernelStatus struct {
	Release   string `json:"release"`
	Major     int    `json:"major"`
	Minor     int    `json:"minor"`
	Supported bool   `json:"supported"`
}

type ModuleStatus struct {
	EmulationInstalled  bool                             `json:"emulationInstalled"`
	EmulationLoaded     bool                             `json:"emulationLoaded"`
	EmulationCompatible bool                             `json:"emulationCompatible"`
	SignaturePresent    bool                             `json:"signaturePresent"`
	Signer              string                           `json:"signer,omitempty"`
	Lockdown            string                           `json:"lockdown"`
	VerificationState   ModuleVerificationState          `json:"verificationState"`
	VerificationClass   ModuleVerificationClassification `json:"verificationClassification,omitempty"`
	VerificationDetail  string                           `json:"verificationDetail,omitempty"`
	VerificationRemedy  string                           `json:"verificationRemediation,omitempty"`
	InspectionError     string                           `json:"inspectionError,omitempty"`
	SigningRequired     bool                             `json:"signingRequired"`
	KVMLoaded           bool                             `json:"kvmLoaded"`
	KVMAMDLoaded        bool                             `json:"kvmAmdLoaded"`
	KVMBusy             bool                             `json:"kvmBusy"`
	ControllerState     string                           `json:"controllerState"`
}

type ModuleTestResponse struct {
	Outcome ModuleVerificationOutcome `json:"outcome"`
	Error   string                    `json:"error,omitempty"`
}

type ManagedActivationFailure struct {
	AppID          string                           `json:"appId"`
	State          ModuleVerificationState          `json:"state"`
	Classification ModuleVerificationClassification `json:"classification"`
	Summary        string                           `json:"summary"`
	Detail         string                           `json:"detail,omitempty"`
	Remediation    string                           `json:"remediation,omitempty"`
}

type ProtonStatus struct {
	Found   bool                `json:"found"`
	Tools   []string            `json:"tools"`
	Invalid []InvalidProtonTool `json:"invalid,omitempty"`
}

type RuntimeState string

const (
	RuntimeStateAbsent      RuntimeState = "absent"
	RuntimeStateAvailable   RuntimeState = "available"
	RuntimeStateInvalid     RuntimeState = "invalid"
	RuntimeStateUnsupported RuntimeState = "unsupported"
)

type RuntimeStatus struct {
	Supported   bool         `json:"supported"`
	Available   bool         `json:"available"`
	State       RuntimeState `json:"state"`
	Path        string       `json:"path"`
	LibraryPath string       `json:"libraryPath"`
	Detail      string       `json:"detail,omitempty"`
}

type LinUwUxStatus struct {
	Available bool          `json:"available"`
	Proton    ProtonStatus  `json:"proton"`
	Runtime   RuntimeStatus `json:"runtime"`
}

type InvalidProtonTool struct {
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

type SystemStatus struct {
	Status  AggregateStatus `json:"status"`
	Path    PathMode        `json:"path"`
	CPU     CPUStatus       `json:"cpu"`
	Kernel  KernelStatus    `json:"kernel"`
	Modules ModuleStatus    `json:"modules"`
	Proton  ProtonStatus    `json:"proton"`
	LinUwUx LinUwUxStatus   `json:"linuwux"`
	Checks  []Check         `json:"checks"`
}

type ManagedGame struct {
	AppID          string       `json:"appId"`
	Name           string       `json:"name"`
	Shortcut       bool         `json:"shortcut"`
	OriginalLaunch string       `json:"originalLaunch"`
	ManagedLaunch  string       `json:"managedLaunch"`
	WrapperPath    string       `json:"wrapperPath"`
	Mode           linuwux.Mode `json:"mode,omitempty"`
}

type ConfigDocument struct {
	Version int                    `json:"version"`
	Games   map[string]ManagedGame `json:"games"`
}

type ManageGameRequest struct {
	Name          string       `json:"name"`
	Shortcut      bool         `json:"shortcut"`
	CurrentLaunch string       `json:"currentLaunch"`
	Mode          linuwux.Mode `json:"mode,omitempty"`
}

type ManageGameResponse struct {
	AppID         string       `json:"appId"`
	ManagedLaunch string       `json:"managedLaunch"`
	WrapperPath   string       `json:"wrapperPath"`
	Mode          linuwux.Mode `json:"mode"`
}

type SessionStartRequest struct {
	AppID string `json:"appId"`
}

type SessionMode string

const (
	SessionModeHypervisor  SessionMode = "hypervisor"
	SessionModePassthrough SessionMode = "passthrough"
)

type SessionStartResponse struct {
	SessionID string      `json:"sessionId"`
	Mode      SessionMode `json:"mode,omitempty"`
}

type LifetimeRequest struct {
	AppID      string `json:"appId"`
	InstanceID uint64 `json:"instanceId"`
	Running    bool   `json:"running"`
}

type Session struct {
	ID         string `json:"id"`
	AppID      string `json:"appId"`
	InstanceID uint64 `json:"instanceId,omitempty"`
	Source     string `json:"source"`
}
