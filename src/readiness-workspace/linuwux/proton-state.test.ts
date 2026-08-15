import { describe, expect, it } from "vitest";
import type { ProtonPreflightResponse, SetupJobSnapshot } from "@/types";
import { emptyProtonDraft, protonDraftReducer } from '@/readiness-workspace/linuwux/proton-state';

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

describe("LinUwUx Proton setup state", () => {
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
});
