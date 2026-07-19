import { describe, expect, it } from "vitest";
import {
  canChangeShortcut,
  groupShortcuts,
  readinessError,
  shortcutActionError,
  shortcutDescription,
  shouldShowShortcutManagement,
} from "./management";
import type { AggregateStatus, Configuration, Game } from "../types";

const emptyConfiguration: Configuration = { version: 1, games: {} };
const managedConfiguration: Configuration = {
  version: 1,
  games: {
    "12": {
      appId: "12",
      name: "Heroic Game",
      shortcut: true,
      originalLaunch: "",
      managedLaunch: "managed",
      wrapperPath: "/wrapper",
    },
  },
};

function game(overrides: Partial<Game>): Game {
  return {
    appId: "1",
    name: "Shortcut",
    shortcut: true,
    enabled: false,
    running: false,
    ...overrides,
  };
}

describe("Shortcut management model", () => {
  it("filters native apps and groups shortcuts deterministically without a search model", () => {
    const sections = groupShortcuts([
      game({ appId: "3", name: "Zulu", enabled: false }),
      game({ appId: "2", name: "Alpha", enabled: true }),
      game({ appId: "4", name: "Beta", enabled: false }),
      game({ appId: "1", name: "Native game", shortcut: false, enabled: true }),
    ]);

    expect(Object.keys(sections)).toEqual(["managed", "available"]);
    expect(sections.managed.map(({ name }) => name)).toEqual(["Alpha"]);
    expect(sections.available.map(({ name }) => name)).toEqual(["Beta", "Zulu"]);
  });

  it.each<AggregateStatus>([
    "native-ready",
    "setup-required",
    "unsupported",
    "recovery-required",
  ])("offers restoration but not new enablement when status is %s", (status) => {
    expect(shouldShowShortcutManagement({ path: "none" }, managedConfiguration)).toBe(true);
    expect(shouldShowShortcutManagement({ path: "none" }, emptyConfiguration)).toBe(false);
    expect(canChangeShortcut(status, game({ enabled: true }))).toBe(true);
    expect(canChangeShortcut(status, game({ enabled: false }))).toBe(false);
  });

  it("offers management for every hypervisor-path state and hides it on a native path without records", () => {
    expect(shouldShowShortcutManagement({ path: "hypervisor" }, emptyConfiguration)).toBe(true);
    expect(shouldShowShortcutManagement({ path: "native" }, emptyConfiguration)).toBe(false);
    expect(canChangeShortcut("hypervisor-ready", game({ enabled: false }))).toBe(true);
    expect(canChangeShortcut("setup-required", game({ enabled: false }))).toBe(false);
  });

  it("does not expose management for stale native-app records", () => {
    const configuration: Configuration = {
      version: 1,
      games: {
        "7": { ...managedConfiguration.games["12"], appId: "7", shortcut: false },
      },
    };
    expect(shouldShowShortcutManagement({ path: "none" }, configuration)).toBe(false);
  });

  it("omits idle shortcut-kind text and includes only contextual state", () => {
    const idle = game({ name: "Heroic" });
    expect(shortcutDescription(idle, "idle", false)).toBeUndefined();
    expect(shortcutDescription(idle, "running", true)).toBe("Running · Updating…");
    expect(shortcutDescription(game({ missing: true }), "idle", false)).toContain("Missing from Steam");
  });

  it("keeps readiness failures generic and action failures shortcut-specific", () => {
    const reason = new Error("backend unavailable");
    expect(readinessError(reason)).toBe("backend unavailable");
    expect(shortcutActionError(game({ name: "Heroic Game" }), true, reason))
      .toBe("Failed to enable “Heroic Game”: backend unavailable");
  });
});
