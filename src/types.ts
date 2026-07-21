export type AggregateStatus =
  | "native-ready"
  | "hypervisor-ready"
  | "setup-required"
  | "recovery-required"
  | "unsupported";

export interface Check {
  id: string;
  ok: boolean;
  label: string;
  detail: string;
  remedy?: string;
}

export interface SystemStatus {
  status: AggregateStatus;
  path: "native" | "hypervisor" | "none";
  cpu: {
    vendor: string;
    modelName: string;
    family: number;
    modelId: number;
    architecture: string;
    generation: string;
    supported: boolean;
    steamDeck: boolean;
    umipPresent: boolean;
    umipRequiredOff: boolean;
    cpuidFaultFlag: boolean;
  };
  kernel: { release: string; major: number; minor: number; supported: boolean };
  modules: {
    emulationInstalled: boolean;
    emulationLoaded: boolean;
    emulationCompatible: boolean;
    kvmLoaded: boolean;
    kvmAmdLoaded: boolean;
    kvmBusy: boolean;
    controllerState: string;
  };
  proton: {
    found: boolean;
    tools: string[];
    invalid?: Array<{ name: string; detail: string }>;
  };
  checks: Check[];
}

export interface Game {
  appId: string;
  name: string;
  shortcut: boolean;
  enabled: boolean;
  running: boolean;
  missing?: boolean;
  conflict?: string;
}

export interface ManagedGame {
  appId: string;
  name: string;
  shortcut: boolean;
  originalLaunch: string;
  managedLaunch: string;
  wrapperPath: string;
}

export interface Configuration {
  version: number;
  games: Record<string, ManagedGame>;
}

export interface ManageResponse {
  appId: string;
  managedLaunch: string;
  wrapperPath: string;
}

export interface RestoreResponse {
  appId: string;
  originalLaunch?: string;
  conflict: boolean;
  message?: string;
}

export type DisplayState = "idle" | "launching" | "running" | "stopping";

export type ProtonCompression = "gzip" | "xz";

export interface ProtonDestination {
  id: string;
  label: string;
}

export interface ProtonArchivePreflight {
  fileName: string;
  compression: ProtonCompression;
  compressedBytes: number;
  destinations: ProtonDestination[];
}

export interface ProtonPreflightResponse {
  preflight: ProtonArchivePreflight;
  responsibility: string;
}

export interface ProtonInstallResult {
  toolName: string;
  destinationId: string;
  sha256: string;
  restartSteam: boolean;
}

export type UMIPBootloader = "limine" | "grub";
export type UMIPSelectionMode = "automatic" | "choice-required" | "manual-only";
export type UMIPCandidateState = "action-required" | "restart-required" | "configured";

export interface UMIPUpdater {
  path: string;
  args: string[];
}

export interface UMIPCandidate {
  bootloader: UMIPBootloader;
  configuration: string;
  updater: UMIPUpdater;
  state: UMIPCandidateState;
  currentValue: string;
  proposedValue: string;
  existingArgument?: string;
  detail: string;
}

export interface UMIPManualOutcome {
  bootloader?: UMIPBootloader;
  reason:
    | "unsupported-syntax"
    | "missing-updater"
    | "conflicting-argument"
    | "unsupported-bootloader";
  detail: string;
}

export interface UMIPInspection {
  liveUmip: boolean;
  selection: UMIPSelectionMode;
  selected?: UMIPBootloader;
  candidates: UMIPCandidate[];
  manual: UMIPManualOutcome[];
}

export interface UMIPApplyResult {
  bootloader: UMIPBootloader;
  restartRequired: boolean;
  backupRetained?: string;
}

export type SetupJobState = "running" | "succeeded" | "failed";

export interface SetupJobSnapshot {
  id: string;
  kind: string;
  state: SetupJobState;
  phase: string;
  progress: number;
  output: string[];
  result?: ProtonInstallResult | UMIPApplyResult | ModuleInstallResult;
  error?: string;
  startedAt: string;
  finishedAt?: string;
}

export interface DependencyPlan {
  manager: string;
  packages: string[];
  previewOutput?: string;
}

export interface ModulePreflightCheck {
  id: string;
  ok: boolean;
  detail: string;
}

export interface ModulePreflight {
  ready: boolean;
  kernelRelease: string;
  buildRoot?: string;
  toolchain?: string;
  packageManager?: string;
  distributionId?: string;
  lockdown: string;
  controllerState: string;
  dependencyPlan?: DependencyPlan;
  dependencyPlanError?: string;
  checks: ModulePreflightCheck[];
}

export interface ModuleIdentity {
  packageName: string;
  packageVersion: string;
  builtModuleName: string;
  destination: string;
  automaticInstall: boolean;
}

export interface ModuleArchiveInspection {
  fileName: string;
  identity: ModuleIdentity;
  entryCount: number;
  expandedBytes: number;
  requiredFiles: string[];
  warning: string;
}

export interface ModuleInstallResult {
  inspection: ModuleArchiveInspection;
  identity: ModuleIdentity;
  kernelRelease: string;
  moduleName: string;
  modulePath: string;
  vermagic: string;
  signer?: string;
  noOp: boolean;
  signingRequired: boolean;
}

export interface ActiveSetupJob {
  active: boolean;
  job?: SetupJobSnapshot;
}

export interface SetupJobEvent {
  type: "setup-job";
  job: SetupJobSnapshot;
}
