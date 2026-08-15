import { describe, expect, it } from "vitest";
import {
  emptyUMIPDraft,
  emptyModuleDraft,
  initialReadinessSelection,
  isFilePickerCancellation,
  readinessDetailKind,
  readinessPageLink,
  readinessSelectionFromPage,
  readinessWorkspaceChecks,
  umipDraftReducer,
  moduleDraftReducer,
} from '@/readiness-workspace/readiness-workspace-state';
import type {
  SetupJobSnapshot,
  UMIPInspection,
  ModuleInstallResult,
} from '@/types';

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

const moduleResult: ModuleInstallResult = {
  inspection: {
    fileName: "cpuid_fault_emulation.zip",
    identity: {
      packageName: "cpuid_fault_emulation",
      packageVersion: "0.1",
      builtModuleName: "cpuid_fault_emulation",
      destination: "/updates",
      automaticInstall: true,
    },
    entryCount: 3,
    expandedBytes: 100,
    requiredFiles: ["dkms.conf", "Makefile"],
    warning: "source warning",
  },
  identity: {
    packageName: "cpuid_fault_emulation",
    packageVersion: "0.1",
    builtModuleName: "cpuid_fault_emulation",
    destination: "/updates",
    automaticInstall: true,
  },
  kernelRelease: "6.18.7-test",
  moduleName: "cpuid_fault_emulation",
  modulePath: "/lib/modules/6.18.7-test/updates/cpuid_fault_emulation.ko",
  vermagic: "6.18.7-test SMP",
  noOp: false,
  signingRequired: false,
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
      { id: "linuwux", ok: false, label: "LinUwUx integration", detail: "missing" },
    ];
    expect(initialReadinessSelection(checks)).toBe("linuwux");
    expect(readinessDetailKind(checks[1])).toBe("linuwux");
  });

  it("keeps setup-related checks in the workspace and leaves CPU and kernel in the QAM", () => {
    const checks = [
      { id: "cpu", ok: true, label: "CPU", detail: "Zen 3" },
      { id: "kernel", ok: true, label: "Linux kernel", detail: "6.18" },
      { id: "umip", ok: true, label: "UMIP", detail: "disabled" },
      { id: "cpuid-fault", ok: true, label: "Native CPUID faulting", detail: "available" },
      { id: "emulation-module", ok: false, label: "CPUID module", detail: "missing" },
      { id: "linuwux", ok: false, label: "LinUwUx integration", detail: "missing" },
    ];

    expect(readinessWorkspaceChecks(checks).map((check) => check.id)).toEqual([
      "umip",
      "emulation-module",
      "linuwux",
    ]);
  });

  it("maps Decky sidebar links back to the selected readiness check", () => {
    const checks = [
      { id: "cpu", ok: true, label: "CPU", detail: "Zen 3" },
      { id: "linuwux", ok: false, label: "LinUwUx integration", detail: "missing" },
    ];
    const route = "/hv-launcher/readiness";

    expect(readinessPageLink(route, "linuwux")).toBe("/hv-launcher/readiness?check=linuwux");
    expect(readinessSelectionFromPage(readinessPageLink(route, "linuwux"), route, checks)).toBe("linuwux");
    expect(readinessSelectionFromPage("/another-route", route, checks)).toBeUndefined();
  });

  it("recognizes picker cancellation", () => {
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

  it("retains the selected module archive through review and terminal job states", () => {
    let state = moduleDraftReducer(emptyModuleDraft, { type: "selection-ready", path: "/home/deck/Downloads/cpuid_fault_emulation.zip" });
    expect(state.stage).toBe("review");
    state = moduleDraftReducer(state, { type: "install-requested" });
    const succeeded: SetupJobSnapshot = {
      id: "module-job",
      kind: "module-install",
      state: "succeeded",
      phase: "complete",
      progress: 100,
      output: ["verified"],
      result: moduleResult,
      startedAt: "2026-07-19T12:00:00Z",
    };
    state = moduleDraftReducer(state, { type: "job-started", job: succeeded });
    expect(state.stage).toBe("complete");
    expect(state.archivePath).toContain("cpuid_fault_emulation.zip");
    expect(state.result).toEqual(moduleResult);
  });

  it("does not attach an unrelated module job over an active installation", () => {
    let state = moduleDraftReducer(emptyModuleDraft, { type: "selection-ready", path: "/tmp/module.zip" });
    state = moduleDraftReducer(state, { type: "install-requested" });
    const running: SetupJobSnapshot = {
      id: "module-running",
      kind: "module-install",
      state: "running",
      phase: "building-module",
      progress: 60,
      output: [],
      startedAt: "2026-07-19T12:00:00Z",
    };
    state = moduleDraftReducer(state, { type: "job-updated", job: running });
    const unrelated = { ...running, id: "other", kind: "proton-install" };
    expect(moduleDraftReducer(state, { type: "job-updated", job: unrelated }).job?.id).toBe("module-running");
  });
});
