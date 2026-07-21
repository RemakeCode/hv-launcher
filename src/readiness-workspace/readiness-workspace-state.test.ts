import { describe, expect, it } from "vitest";
import {
  emptyProtonDraft,
  emptyUMIPDraft,
  initialReadinessSelection,
  isFilePickerCancellation,
  isSupportedProtonArchive,
  protonDraftReducer,
  readinessDetailKind,
  readinessPageLink,
  readinessSelectionFromPage,
  readinessWorkspaceChecks,
  umipDraftReducer,
} from "./readiness-workspace-state";
import type {
  ProtonPreflightResponse,
  SetupJobSnapshot,
  UMIPInspection,
} from "../types";

const selection: ProtonPreflightResponse = {
  responsibility: "Confirm the source",
  preflight: {
    fileName: "GE-Proton-LinUwUx.tar.xz",
    compression: "xz",
    compressedBytes: 100,
    destinations: [
      { id: "native", label: "Steam (native)" },
      { id: "flatpak", label: "Steam (Flatpak)" },
    ],
  },
};

function job(state: SetupJobSnapshot["state"]): SetupJobSnapshot {
  return {
    id: "job", kind: "proton-install", state, phase: state === "running" ? "installing" : "complete",
    progress: state === "running" ? 50 : 100, output: [], startedAt: "2026-07-18T12:00:00Z",
    result: state === "succeeded" ? {
      toolName: "GE-Proton11-1-LinUwUx",
      destinationId: "native",
      sha256: "abc123",
      restartSteam: true,
    } : undefined,
  };
}

const limineInspection: UMIPInspection = {
  liveUmip: true,
  selection: "automatic",
  selected: "limine",
  candidates: [{
    bootloader: "limine",
    configuration: "/etc/default/limine",
    updater: { path: "/usr/bin/limine-update", args: [] },
    state: "action-required",
    currentValue: "quiet",
    proposedValue: "quiet clearcpuid=514",
    detail: "clearcpuid=514 can be added after review.",
  }],
  manual: [],
};

function umipJob(state: SetupJobSnapshot["state"]): SetupJobSnapshot {
  return {
    id: "umip-job",
    kind: "umip-apply",
    state,
    phase: state === "running" ? "updating-configuration" : state === "succeeded" ? "complete" : "failed",
    progress: state === "running" ? 35 : 100,
    output: [],
    error: state === "failed" ? "updater failed; configuration was rolled back" : undefined,
    result: state === "succeeded" ? { bootloader: "limine", restartRequired: true } : undefined,
    startedAt: "2026-07-19T12:00:00Z",
  };
}

describe("readiness workspace state", () => {
  it("selects an outstanding check without starting a mutation", () => {
    const checks = [
      { id: "cpu", ok: true, label: "CPU", detail: "ready" },
      { id: "proton", ok: false, label: "Proton", detail: "missing" },
    ];
    expect(initialReadinessSelection(checks)).toBe("proton");
    expect(readinessDetailKind(checks[1])).toBe("proton");
    expect(emptyProtonDraft).toEqual({ stage: "idle" });
  });

  it("keeps setup-related checks in the workspace and leaves CPU and kernel in the QAM", () => {
    const checks = [
      { id: "cpu", ok: true, label: "CPU", detail: "Zen 3" },
      { id: "kernel", ok: true, label: "Linux kernel", detail: "6.18" },
      { id: "umip", ok: true, label: "UMIP", detail: "disabled" },
      { id: "cpuid-fault", ok: true, label: "Native CPUID faulting", detail: "available" },
      { id: "emulation-module", ok: false, label: "CPUID module", detail: "missing" },
      { id: "proton", ok: false, label: "Proton", detail: "missing" },
    ];

    expect(readinessWorkspaceChecks(checks).map((check) => check.id)).toEqual([
      "umip",
      "emulation-module",
      "proton",
    ]);
  });

  it("maps Decky sidebar links back to the selected readiness check", () => {
    const checks = [
      { id: "cpu", ok: true, label: "CPU", detail: "Zen 3" },
      { id: "proton", ok: false, label: "Proton", detail: "missing" },
    ];
    const route = "/hv-launcher/readiness";

    expect(readinessPageLink(route, "proton")).toBe("/hv-launcher/readiness?check=proton");
    expect(readinessSelectionFromPage(readinessPageLink(route, "proton"), route, checks)).toBe("proton");
    expect(readinessSelectionFromPage("/another-route", route, checks)).toBeUndefined();
  });

  it("requires the user to choose among multiple Steam roots", () => {
    let state = protonDraftReducer(emptyProtonDraft, { type: "selection-ready", path: "/tmp/tool.tar.xz", selection });
    expect(state.stage).toBe("confirm");
    expect(state.destinationId).toBeUndefined();
    state = protonDraftReducer(state, { type: "destination-selected", destinationId: selection.preflight.destinations[1].id });
    expect(state.destinationId).toBe(selection.preflight.destinations[1].id);
  });

  it("selects the destination automatically only when exactly one Steam root exists", () => {
    const singleDestination = {
      ...selection,
      preflight: { ...selection.preflight, destinations: [selection.preflight.destinations[0]] },
    };
    const state = protonDraftReducer(emptyProtonDraft, {
      type: "selection-ready",
      path: "/tmp/tool.tar.xz",
      selection: singleDestination,
    });

    expect(state.destinationId).toBe("native");
  });

  it("discards a stale review while inspecting a replacement and restores it on cancellation", () => {
    const reviewed = protonDraftReducer(emptyProtonDraft, {
      type: "selection-ready",
      path: "/tmp/tool.tar.xz",
      selection,
    });
    const selecting = protonDraftReducer(reviewed, { type: "selection-started" });

    expect(selecting).toEqual({ stage: "selecting" });
    expect(protonDraftReducer(selecting, { type: "selection-cancelled", previous: reviewed })).toBe(reviewed);
    expect(protonDraftReducer(selecting, { type: "failed", error: "invalid archive" })).toEqual({
      stage: "failure",
      error: "invalid archive",
    });
  });

  it("returns a completed install to a reusable flow while retaining its result", () => {
    const reviewed = protonDraftReducer(emptyProtonDraft, { type: "selection-ready", path: "/tmp/tool.tar.xz", selection });
    const requested = protonDraftReducer(reviewed, { type: "install-requested" });
    expect(requested.stage).toBe("installing");
    expect(requested.job).toBeUndefined();
    const installing = protonDraftReducer(requested, { type: "job-started", job: job("running") });
    expect(installing.stage).toBe("installing");
    expect(protonDraftReducer(installing, { type: "job-updated", job: job("succeeded") }).stage).toBe("completing");
    const failedJob = { ...job("failed"), error: "disk is full" };
    const failed = protonDraftReducer(installing, { type: "job-updated", job: failedJob });
    expect(failed.stage).toBe("failure");
    expect(failed.selection).toBe(selection);
    const completed = protonDraftReducer(installing, { type: "job-updated", job: job("succeeded") });
    const displayed = protonDraftReducer(completed, { type: "completion-displayed" });
    expect(displayed).toEqual({
      stage: "idle",
      lastInstall: job("succeeded").result,
    });

    const selectingAgain = protonDraftReducer(displayed, { type: "selection-started" });
    expect(selectingAgain).toEqual({ stage: "selecting", lastInstall: job("succeeded").result });
    const selectedAgain = protonDraftReducer(selectingAgain, {
      type: "selection-ready",
      path: "/tmp/another-tool.tar.xz",
      selection,
    });
    expect(selectedAgain.stage).toBe("confirm");
    expect(selectedAgain.lastInstall).toEqual(job("succeeded").result);
  });

  it("preserves terminal state when the started-job attachment already completed", () => {
    const reviewed = protonDraftReducer(emptyProtonDraft, {
      type: "selection-ready",
      path: "/tmp/tool.tar.xz",
      selection,
    });
    const requested = protonDraftReducer(reviewed, { type: "install-requested" });

    const succeeded = protonDraftReducer(requested, { type: "job-started", job: job("succeeded") });
    expect(succeeded.stage).toBe("completing");
    expect(succeeded.job).toEqual(job("succeeded"));

    const failedJob = { ...job("failed"), error: "disk is full" };
    const failed = protonDraftReducer(requested, { type: "job-started", job: failedJob });
    expect(failed.stage).toBe("failure");
    expect(failed.error).toBe("disk is full");
  });

  it("handles supported extensions and picker cancellation", () => {
    expect(isSupportedProtonArchive("a.tar.gz")).toBe(true);
    expect(isSupportedProtonArchive("a.tgz")).toBe(true);
    expect(isSupportedProtonArchive("a.tar.xz")).toBe(true);
    expect(isSupportedProtonArchive("a.zip")).toBe(false);
    expect(isFilePickerCancellation(new Error("Picker cancelled"))).toBe(true);
    expect(isFilePickerCancellation(new Error("permission denied"))).toBe(false);
  });

  it("selects one UMIP candidate without starting a mutation", () => {
    const state = umipDraftReducer(emptyUMIPDraft, {
      type: "inspection-loaded",
      inspection: limineInspection,
    });

    expect(state.stage).toBe("idle");
    expect(state.selected).toBe("limine");
    expect(state.job).toBeUndefined();
  });

  it("retains the explicit bootloader choice", () => {
    const choice: UMIPInspection = {
      ...limineInspection,
      selection: "choice-required",
      selected: undefined,
      candidates: [
        ...limineInspection.candidates,
        {
          bootloader: "grub",
          configuration: "/etc/default/grub",
          updater: { path: "/usr/sbin/update-grub", args: [] },
          state: "action-required",
          currentValue: "quiet",
          proposedValue: "quiet clearcpuid=514",
          detail: "clearcpuid=514 can be added after review.",
        },
      ],
    };
    let state = umipDraftReducer(emptyUMIPDraft, { type: "inspection-loaded", inspection: choice });
    expect(state.selected).toBeUndefined();
    state = umipDraftReducer(state, { type: "bootloader-selected", bootloader: "grub" });
    expect(state.selected).toBe("grub");
    expect(state.stage).toBe("idle");
  });

  it("does not offer the UMIP mutation again while a restart is required", () => {
    const configured: UMIPInspection = {
      ...limineInspection,
      candidates: [{
        ...limineInspection.candidates[0],
        state: "restart-required",
        existingArgument: "clearcpuid=514",
      }],
    };
    const state = umipDraftReducer(emptyUMIPDraft, { type: "inspection-loaded", inspection: configured });

    expect(state.stage).toBe("restart-required");
  });

  it("preserves terminal UMIP state when job attachment races completion", () => {
    let state = umipDraftReducer(emptyUMIPDraft, {
      type: "inspection-loaded",
      inspection: limineInspection,
    });
    state = umipDraftReducer(state, { type: "apply-requested" });

    const succeeded = umipDraftReducer(state, { type: "job-started", job: umipJob("succeeded") });
    expect(succeeded.stage).toBe("restart-required");

    const failed = umipDraftReducer(state, { type: "job-started", job: umipJob("failed") });
    expect(failed.stage).toBe("failure");
    expect(failed.error).toContain("rolled back");
  });
});
