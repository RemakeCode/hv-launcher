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
  proton: { found: boolean; tools: string[] };
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
