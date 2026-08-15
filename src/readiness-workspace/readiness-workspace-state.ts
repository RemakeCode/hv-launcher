import type {
  Check,
  ModuleInstallResult,
  SetupJobSnapshot,
  UMIPBootloader,
  UMIPInspection,
} from '@/types';

export type UMIPFlowStage =
  | "loading"
  | "idle"
  | "applying"
  | "restart-required"
  | "failure";

export interface UMIPDraft {
  stage: UMIPFlowStage;
  inspection?: UMIPInspection;
  selected?: UMIPBootloader;
  job?: SetupJobSnapshot;
  error?: string;
}

export const emptyUMIPDraft: UMIPDraft = { stage: "loading" };

export type ModuleFlowStage = "idle" | "selecting" | "review" | "installing" | "complete" | "failure";

export interface ModuleDraft {
  stage: ModuleFlowStage;
  archivePath?: string;
  job?: SetupJobSnapshot;
  result?: ModuleInstallResult;
  error?: string;
}

export const emptyModuleDraft: ModuleDraft = { stage: "idle" };

const readinessWorkspaceCheckIds = new Set(["umip", "emulation-module", "linuwux"]);

export type UMIPDraftAction =
  | { type: "inspection-loaded"; inspection: UMIPInspection }
  | { type: "inspection-failed"; error: string }
  | { type: "bootloader-selected"; bootloader: UMIPBootloader }
  | { type: "apply-requested" }
  | { type: "job-started"; job: SetupJobSnapshot }
  | { type: "job-updated"; job: SetupJobSnapshot }
  | { type: "failed"; error: string };

export type ModuleDraftAction =
  | { type: "selection-started" }
  | { type: "selection-cancelled" }
  | { type: "selection-ready"; path: string }
  | { type: "preflight-failed"; error: string }
  | { type: "install-requested" }
  | { type: "job-started"; job: SetupJobSnapshot }
  | { type: "job-updated"; job: SetupJobSnapshot }
  | { type: "failed"; error: string };

function umipStageForInspection(
  inspection: UMIPInspection,
  selected?: UMIPBootloader,
): UMIPFlowStage {
  const candidate = inspection.candidates.find((item) => item.bootloader === selected);
  return candidate?.state === "restart-required" ? "restart-required" : "idle";
}

function attachUMIPJob(state: UMIPDraft, job: SetupJobSnapshot): UMIPDraft {
  if (job.state === "running") {
    return { ...state, stage: "applying", job, error: undefined };
  }
  if (job.state === "succeeded") {
    return { ...state, stage: "restart-required", job, error: undefined };
  }
  return { ...state, stage: "failure", job, error: job.error ?? "UMIP setup did not complete." };
}

export function umipDraftReducer(state: UMIPDraft, action: UMIPDraftAction): UMIPDraft {
  switch (action.type) {
    case "inspection-loaded": {
      const inspection = action.inspection;
      const retainedSelection = inspection.candidates.some((item) => item.bootloader === state.selected)
        ? state.selected
        : undefined;
      const selected = retainedSelection ?? (inspection.selection === "automatic"
        ? inspection.selected ?? inspection.candidates[0]?.bootloader
        : undefined);
      if (state.job?.state === "running") {
        return { ...state, inspection, selected };
      }
      if (state.job?.state === "succeeded") {
        return { ...state, inspection, selected, stage: "restart-required" };
      }
      if (state.stage === "failure") {
        return { ...state, inspection, selected };
      }
      return {
        ...state,
        inspection,
        selected,
        stage: umipStageForInspection(inspection, selected),
        error: undefined,
      };
    }
    case "inspection-failed":
      return { ...state, stage: "failure", error: action.error };
    case "bootloader-selected":
      return {
        ...state,
        selected: action.bootloader,
        stage: state.inspection
          ? umipStageForInspection(state.inspection, action.bootloader)
          : "idle",
        error: undefined,
      };
    case "apply-requested":
      return { ...state, stage: "applying", job: undefined, error: undefined };
    case "job-started":
      return action.job.kind === "umip-apply" ? attachUMIPJob(state, action.job) : state;
    case "job-updated":
      if (action.job.kind !== "umip-apply") return state;
      if (state.job && state.job.id !== action.job.id && state.job.state === "running") return state;
      return attachUMIPJob(state, action.job);
    case "failed":
      return { ...state, stage: "failure", error: action.error };
  }
}

function attachModuleJob(state: ModuleDraft, job: SetupJobSnapshot): ModuleDraft {
  if (job.state === "running") return { ...state, stage: "installing", job, error: undefined };
  if (job.state === "succeeded") {
    return {
      ...state,
      stage: "complete",
      job,
      result: job.result as ModuleInstallResult | undefined,
      error: undefined,
    };
  }
  return { ...state, stage: "failure", job, error: job.error ?? "CPUID module setup did not complete." };
}

export function moduleDraftReducer(state: ModuleDraft, action: ModuleDraftAction): ModuleDraft {
  switch (action.type) {
    case "selection-started":
      return { ...state, stage: "selecting", error: undefined };
    case "selection-cancelled":
      return { ...state, stage: state.archivePath ? "review" : "idle" };
    case "selection-ready":
      return { ...state, stage: "review", archivePath: action.path, error: undefined };
    case "preflight-failed":
      return { ...state, error: action.error };
    case "install-requested":
      return { ...state, stage: "installing", job: undefined, error: undefined };
    case "job-started":
      if (state.job?.id === action.job.id && state.job.state !== "running") return state;
      return attachModuleJob(state, action.job);
    case "job-updated":
      if (action.job.kind !== "module-install") return state;
      if (state.job && state.job.id !== action.job.id && state.job.state === "running") return state;
      return attachModuleJob(state, action.job);
    case "failed":
      return { ...state, stage: "failure", error: action.error };
  }
}

export function initialReadinessSelection(checks: Check[]): string {
  return checks.find((check) => !check.ok)?.id ?? checks[0]?.id ?? "";
}

export function readinessWorkspaceChecks(checks: Check[]): Check[] {
  return checks.filter((check) => readinessWorkspaceCheckIds.has(check.id));
}

export function readinessPageLink(route: string, checkId: string): string {
  return `${route}?check=${encodeURIComponent(checkId)}`;
}

export function readinessSelectionFromPage(
  page: string,
  route: string,
  checks: Check[],
): string | undefined {
  return checks.find((check) => readinessPageLink(route, check.id) === page)?.id;
}

export function readinessDetailKind(check: Check): "ready" | "manual" | "linuwux" {
  if (check.ok) return "ready";
  return check.id === "linuwux" ? "linuwux" : "manual";
}

export function isFilePickerCancellation(reason: unknown): boolean {
  if (!reason) return true;
  const message = reason instanceof Error ? reason.message : String(reason);
  return /cancel|dismiss|closed/i.test(message);
}
