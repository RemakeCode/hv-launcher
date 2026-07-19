import { beforeEach, describe, expect, it, vi } from "vitest";
import type { SetupJobSnapshot } from "./types";

const { getActiveSetupJob, getSetupJob } = vi.hoisted(() => ({
  getActiveSetupJob: vi.fn(),
  getSetupJob: vi.fn(),
}));

vi.mock("./api", () => ({
  BASE_URL: "http://127.0.0.1:42991/v1",
  getActiveSetupJob,
  getSetupJob,
}));

import { SetupEventStore } from "./setup-events";

class FakeEventStream {
  onopen: ((event: Event) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  closed = false;
  listeners = new Map<string, (event: MessageEvent<string>) => void>();

  close() {
    this.closed = true;
  }

  addEventListener(type: string, listener: (event: MessageEvent<string>) => void) {
    this.listeners.set(type, listener);
  }

  emit(job: SetupJobSnapshot) {
    this.listeners.get("setup-job")?.({
      data: JSON.stringify({ type: "setup-job", job }),
    } as MessageEvent<string>);
  }
}

function snapshot(state: SetupJobSnapshot["state"]): SetupJobSnapshot {
  return {
    id: "job-1",
    kind: "proton-install",
    state,
    phase: state === "running" ? "installing" : "complete",
    progress: state === "running" ? 40 : 100,
    output: [],
    startedAt: "2026-07-18T12:00:00Z",
  };
}

describe("plugin-lifetime setup events", () => {
  beforeEach(() => {
    getActiveSetupJob.mockReset();
    getSetupJob.mockReset();
  });

  it("reconciles snapshots, updates subscribers, and deduplicates terminal notifications", async () => {
    const stream = new FakeEventStream();
    const running = snapshot("running");
    getActiveSetupJob.mockResolvedValue({ active: true, job: running });
    getSetupJob.mockResolvedValue(running);
    const terminal = vi.fn();
    const listener = vi.fn();
    const store = new SetupEventStore(() => stream);
    store.subscribe(listener);
    store.start(terminal);

    await vi.waitFor(() => expect(listener).toHaveBeenCalledWith(running));
    const success = snapshot("succeeded");
    stream.emit(success);
    stream.emit(success);
    expect(listener).toHaveBeenLastCalledWith(success);
    expect(terminal).toHaveBeenCalledOnce();
    expect(store.current("proton-install")).toEqual(success);

    store.dismiss("proton-install");
    expect(store.current("proton-install")).toBeUndefined();
    const replayed = vi.fn();
    store.subscribe(replayed);
    expect(replayed).not.toHaveBeenCalled();

    stream.onopen?.(new Event("open"));
    await vi.waitFor(() => expect(getActiveSetupJob).toHaveBeenCalledTimes(2));
    store.stop();
    expect(stream.closed).toBe(true);
  });
});
