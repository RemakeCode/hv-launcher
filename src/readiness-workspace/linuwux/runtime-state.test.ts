import { describe, expect, it } from "vitest";
import { runtimeAction } from '@/readiness-workspace/linuwux/runtime-state';
import type { RuntimeSetupStatus } from "@/types";

describe("LinUwUx runtime setup state", () => {
  it("maps absent, invalid, unknown-version, update, and current states to explicit actions", () => {
    expect(runtimeAction(setup({ state: "absent", available: false, versionKnown: false }))).toBe("install");
    expect(runtimeAction(setup({ state: "invalid", available: false, versionKnown: false }))).toBe("repair");
    expect(runtimeAction(setup({ state: "available", available: true, versionKnown: false }))).toBe("repair");
    expect(runtimeAction(setup({
      state: "available", available: true, versionKnown: true, version: "26.08.14",
      updateState: "update-available", latestVersion: "26.08.14.1",
    }))).toBe("update");
    expect(runtimeAction(setup({
      state: "available", available: true, versionKnown: true, version: "26.08.14.1",
      updateState: "current", latestVersion: "26.08.14.1",
    }))).toBeUndefined();
  });

  it("does not offer installation on an unsupported architecture", () => {
    expect(runtimeAction(setup({
      supported: false, state: "unsupported", available: false, versionKnown: false,
    }))).toBeUndefined();
  });
});

function setup(overrides: Partial<RuntimeSetupStatus["runtime"]>): RuntimeSetupStatus {
  return {
    repository: "brcly/linuwux-runtime",
    runtime: {
      supported: true,
      available: false,
      state: "absent",
      path: "/home/deck/.local/bin/linuwux",
      libraryPath: "/home/deck/.local/lib/liblinuwux.so",
      versionKnown: false,
      updateState: "unknown",
      ...overrides,
    },
  };
}
