package model

type AggregateStatus string

const (
	StatusNativeReady     AggregateStatus = "native-ready"
	StatusHypervisorReady AggregateStatus = "hypervisor-ready"
	StatusSetupRequired   AggregateStatus = "setup-required"
	StatusRecovery        AggregateStatus = "recovery-required"
	StatusUnsupported     AggregateStatus = "unsupported"
)

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
	EmulationInstalled  bool   `json:"emulationInstalled"`
	EmulationLoaded     bool   `json:"emulationLoaded"`
	EmulationCompatible bool   `json:"emulationCompatible"`
	KVMLoaded           bool   `json:"kvmLoaded"`
	KVMAMDLoaded        bool   `json:"kvmAmdLoaded"`
	KVMBusy             bool   `json:"kvmBusy"`
	ControllerState     string `json:"controllerState"`
}

type ProtonStatus struct {
	Found   bool                `json:"found"`
	Tools   []string            `json:"tools"`
	Invalid []InvalidProtonTool `json:"invalid,omitempty"`
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
	Checks  []Check         `json:"checks"`
}

type ManagedGame struct {
	AppID          string `json:"appId"`
	Name           string `json:"name"`
	Shortcut       bool   `json:"shortcut"`
	OriginalLaunch string `json:"originalLaunch"`
	ManagedLaunch  string `json:"managedLaunch"`
	WrapperPath    string `json:"wrapperPath"`
}

type ConfigDocument struct {
	Version int                    `json:"version"`
	Games   map[string]ManagedGame `json:"games"`
}

type ManageGameRequest struct {
	Name          string `json:"name"`
	Shortcut      bool   `json:"shortcut"`
	CurrentLaunch string `json:"currentLaunch"`
}

type ManageGameResponse struct {
	AppID         string `json:"appId"`
	ManagedLaunch string `json:"managedLaunch"`
	WrapperPath   string `json:"wrapperPath"`
}

type RestoreResponse struct {
	AppID          string `json:"appId"`
	OriginalLaunch string `json:"originalLaunch,omitempty"`
	Conflict       bool   `json:"conflict"`
	Message        string `json:"message,omitempty"`
}

type RestoreRequest struct {
	CurrentLaunch string `json:"currentLaunch"`
}

type SessionStartRequest struct {
	AppID string `json:"appId"`
}

type SessionStartResponse struct {
	SessionID string `json:"sessionId"`
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
