import type {
  Check,
  ProtonInstallResult,
  ProtonSelectionResponse,
  SetupJobSnapshot,
  UMIPBootloader,
  UMIPInspection,
} from "../types";

export type ProtonFlowStage =
  | "idle"
  | "selecting"
  | "confirm"
  | "installing"
  | "completing"
  | "failure";

export interface ProtonDraft {
  stage: ProtonFlowStage;
  archivePath?: string;
  selection?: ProtonSelectionResponse;
  destinationId?: string;
  job?: SetupJobSnapshot;
  lastInstall?: ProtonInstallResult;
  error?: string;
}

export const emptyProtonDraft: ProtonDraft = { stage: "idle" };

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

const readinessWorkspaceCheckIds = new Set(["umip", "emulation-module", "proton"]);

export type ProtonDraftAction =
  | { type: "selection-started" }
  | { type: "selection-cancelled"; previous: ProtonDraft }
  | { type: "selection-ready"; path: string; selection: ProtonSelectionResponse }
  | { type: "destination-selected"; destinationId: string }
  | { type: "install-requested" }
  | { type: "job-started"; job: SetupJobSnapshot }
  | { type: "job-updated"; job: SetupJobSnapshot }
  | { type: "completion-displayed" }
  | { type: "failed"; error: string };

export type UMIPDraftAction =
  | { type: "inspection-loaded"; inspection: UMIPInspection }
  | { type: "inspection-failed"; error: string }
  | { type: "bootloader-selected"; bootloader: UMIPBootloader }
  | { type: "apply-requested" }
  | { type: "job-started"; job: SetupJobSnapshot }
  | { type: "job-updated"; job: SetupJobSnapshot }
  | { type: "failed"; error: string };

function protonStageForJob(job: SetupJobSnapshot): ProtonFlowStage {
  if (job.state === "running") return "installing";
  return job.state === "succeeded" ? "completing" : "failure";
}

function attachProtonJob(state: ProtonDraft, job: SetupJobSnapshot): ProtonDraft {
  return {
    ...state,
    stage: protonStageForJob(job),
    job,
    error: job.error,
  };
}

export function protonDraftReducer(
  state: ProtonDraft,
  action: ProtonDraftAction,
): ProtonDraft {
  switch (action.type) {
    case "selection-started":
      return { stage: "selecting", lastInstall: state.lastInstall };
    case "selection-cancelled":
      return action.previous;
    case "selection-ready":
      return {
        stage: "confirm",
        lastInstall: state.lastInstall,
        archivePath: action.path,
        selection: action.selection,
        destinationId: action.selection.preflight.destinations.length === 1
          ? action.selection.preflight.destinations[0].id
          : undefined,
      };
    case "destination-selected":
      return { ...state, destinationId: action.destinationId };
    case "install-requested":
      return { ...state, stage: "installing", job: undefined, error: undefined };
    case "job-started":
      if (state.job?.id === action.job.id && state.job.state !== "running") return state;
      return attachProtonJob(state, action.job);
    case "job-updated":
      if (action.job.kind !== "proton-install") return state;
      if (state.job && state.job.id !== action.job.id && state.job.state === "running") return state;
      return attachProtonJob(state, action.job);
    case "completion-displayed":
      return state.stage === "completing"
        ? {
            stage: "idle",
            lastInstall: state.job?.kind === "proton-install"
              ? state.job.result as ProtonInstallResult | undefined
              : undefined,
          }
        : state;
    case "failed":
      return { ...state, stage: "failure", error: action.error };
  }
}

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

export function readinessDetailKind(check: Check): "ready" | "manual" | "proton" {
  if (check.ok) return "ready";
  return check.id === "proton" ? "proton" : "manual";
}

export function isSupportedProtonArchive(path: string): boolean {
  const lower = path.toLowerCase();
  return lower.endsWith(".tar.gz") || lower.endsWith(".tgz") || lower.endsWith(".tar.xz");
}

export function isFilePickerCancellation(reason: unknown): boolean {
  if (!reason) return true;
  const message = reason instanceof Error ? reason.message : String(reason);
  return /cancel|dismiss|closed/i.test(message);
}
