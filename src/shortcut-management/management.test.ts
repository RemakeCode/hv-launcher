import { describe, expect, it } from "vitest";
import {
  groupShortcuts,
  shouldShowShortcutManagement,
} from "./management";
import type { Configuration, Game } from "../types";

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

  it("shows management for existing managed shortcuts on non-hypervisor paths", () => {
    expect(shouldShowShortcutManagement({ path: "none" }, managedConfiguration)).toBe(true);
    expect(shouldShowShortcutManagement({ path: "none" }, emptyConfiguration)).toBe(false);
  });

  it("offers management on the hypervisor path and for existing managed shortcuts", () => {
    expect(shouldShowShortcutManagement({ path: "hypervisor" }, emptyConfiguration)).toBe(true);
    expect(shouldShowShortcutManagement({ path: "native" }, emptyConfiguration)).toBe(false);
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
});
