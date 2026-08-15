import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  disableManagedGame,
  discoverGames,
  observeSteamLifetime,
  observeSteamOverviews,
  readLaunchValue,
  reconfigureManagedGame,
  SteamLibraryLoadingError,
  type MaterializedAppStore,
  type SteamBridge,
} from '@/steam';
import type { Configuration } from '@/types';

const api = vi.hoisted(() => ({
  disableGame: vi.fn(),
  enableGame: vi.fn(),
  getConfiguration: vi.fn(),
  postLifetime: vi.fn(),
  reconfigureGame: vi.fn(),
}));

vi.mock("./api", () => api);

beforeEach(() => {
  vi.clearAllMocks();
});

function bridgeWithoutOverview() {
  let lifetime: ((notification: { unAppID: number; nInstanceID: number; bRunning: boolean }) => void) | undefined;
  const unregister = vi.fn();
  const registerLifetime = vi.fn((callback: NonNullable<typeof lifetime>) => {
    lifetime = callback;
    return { unregister };
  });
  const bridge: SteamBridge = {
    Apps: {
      SetAppLaunchOptions: vi.fn(),
      SetShortcutLaunchOptions: vi.fn(),
    },
    GameSessions: {
      RegisterForAppLifetimeNotifications: registerLifetime,
    },
  };
  return {
    bridge,
    emit: (value: Parameters<NonNullable<typeof lifetime>>[0]) => lifetime?.(value),
    registerLifetime,
    unregister,
  };
}

describe("Steam observation", () => {
  it("keeps lifetime cleanup working when overview callbacks are unavailable", async () => {
    const fixture = bridgeWithoutOverview();
    const sendLifetime = vi.fn(async () => undefined);
    const cleanup = observeSteamLifetime({ bridge: fixture.bridge, sendLifetime });
    fixture.emit({ unAppID: 10, nInstanceID: 22, bRunning: false });
    await vi.waitFor(() => expect(sendLifetime).toHaveBeenCalledWith("10", 22, false));
    cleanup();
    expect(fixture.unregister).toHaveBeenCalledOnce();
  });

  it("retries unresolved App ID zero and cancels cleanly", async () => {
    vi.useFakeTimers();
    const fixture = bridgeWithoutOverview();
    const sendLifetime = vi.fn(async () => ({ status: "unresolved" }));
    const cleanup = observeSteamLifetime({ bridge: fixture.bridge, sendLifetime });
    fixture.emit({ unAppID: 0, nInstanceID: 7, bRunning: true });
    await vi.runAllTimersAsync();
    expect(sendLifetime).toHaveBeenCalledTimes(4);
    cleanup();
    vi.useRealTimers();
  });

  it("reports lifetime forwarding failures without an unhandled rejection", async () => {
    const fixture = bridgeWithoutOverview();
    const onError = vi.fn();
    const cleanup = observeSteamLifetime({
      bridge: fixture.bridge,
      onError,
      sendLifetime: vi.fn(async () => {
        throw new Error("backend unavailable");
      }),
    });
    fixture.emit({ unAppID: 10, nInstanceID: 22, bRunning: true });
    await vi.waitFor(() => expect(onError).toHaveBeenCalledWith(expect.objectContaining({ message: "backend unavailable" })));
    cleanup();
  });

  it("keeps overview observation separate from lifetime forwarding", () => {
    const fixture = bridgeWithoutOverview();
    const overviewUnregister = vi.fn();
    let emitOverview: (() => void) | undefined;
    fixture.bridge.Apps.RegisterForAppOverviewChanges = (callback) => {
      emitOverview = () => callback(new ArrayBuffer(0));
      return { unregister: overviewUnregister };
    };
    const onOverview = vi.fn();
    const cleanup = observeSteamOverviews(onOverview, fixture.bridge);
    emitOverview?.();
    expect(onOverview).toHaveBeenCalledOnce();
    cleanup();
    expect(overviewUnregister).toHaveBeenCalledOnce();
    expect(fixture.unregister).not.toHaveBeenCalled();
  });

  it("keeps plugin lifetime forwarding active across repeated route observer mounts", async () => {
    const fixture = bridgeWithoutOverview();
    const overviewUnregister = vi.fn();
    fixture.bridge.Apps.RegisterForAppOverviewChanges = vi.fn(() => ({ unregister: overviewUnregister }));
    const sendLifetime = vi.fn(async () => undefined);
    const stopLifetime = observeSteamLifetime({ bridge: fixture.bridge, sendLifetime });

    const closeFirstRoute = observeSteamOverviews(vi.fn(), fixture.bridge);
    closeFirstRoute();
    const closeSecondRoute = observeSteamOverviews(vi.fn(), fixture.bridge);
    closeSecondRoute();
    fixture.emit({ unAppID: 77, nInstanceID: 4, bRunning: false });

    await vi.waitFor(() => expect(sendLifetime).toHaveBeenCalledOnce());
    expect(fixture.registerLifetime).toHaveBeenCalledOnce();
    expect(overviewUnregister).toHaveBeenCalledTimes(2);
    expect(fixture.unregister).not.toHaveBeenCalled();
    stopLifetime();
    expect(fixture.unregister).toHaveBeenCalledOnce();
  });
});

describe("allApps discovery", () => {
  const configuration: Configuration = {
    version: 1,
    games: {
      "42": {
        appId: "42",
        name: "Stale Game",
        shortcut: false,
        originalLaunch: "",
        managedLaunch: "managed",
        wrapperPath: "/wrapper",
      },
    },
  };

  it("uses initialized allApps and preserves unsigned shortcut IDs", () => {
    const store: MaterializedAppStore = {
      m_bIsInitialized: true,
      allApps: [
        {
          appid: 2650715882,
          display_name: "Crimson Desert",
          app_type: 0x40000000,
          visible_in_game_list: true,
          per_client_data: [{ installed: true, display_status: 11 }],
        },
        {
          appid: 10,
          display_name: "Steam Game",
          app_type: 1,
          visible_in_game_list: true,
          per_client_data: [{ installed: true, display_status: 4 }],
        },
        {
          appid: 20,
          display_name: "Uninstalled",
          app_type: 1,
          visible_in_game_list: true,
          per_client_data: [{ installed: false }],
        },
      ],
      GetAppOverviewByAppID: () => null,
    };
    const games = discoverGames(configuration, store);
    expect(games.map((game) => game.appId)).toEqual(["2650715882", "42", "10"]);
    expect(games.find((game) => game.appId === "2650715882")?.shortcut).toBe(true);
    expect(games.find((game) => game.appId === "10")?.running).toBe(true);
    expect(games.find((game) => game.appId === "42")?.missing).toBe(true);
  });

  it("reports loading without another discovery fallback", () => {
    expect(() =>
      discoverGames(configuration, {
        m_bIsInitialized: false,
        allApps: [],
        GetAppOverviewByAppID: () => null,
      }),
    ).toThrow(SteamLibraryLoadingError);
    expect(() =>
      discoverGames(configuration, {
        m_bIsInitialized: true,
        GetAppOverviewByAppID: () => null,
      }),
    ).toThrow(SteamLibraryLoadingError);
  });

  it("reports loading when Decky app stores are unavailable", () => {
    expect(() => discoverGames(configuration)).toThrow(SteamLibraryLoadingError);
  });

  it("preserves a configured missing shortcut for restoration while excluding stale native apps", () => {
    const configured: Configuration = {
      version: 1,
      games: {
        "42": configuration.games["42"],
        "43": {
          appId: "43",
          name: "Missing Heroic Shortcut",
          shortcut: true,
          originalLaunch: "",
          managedLaunch: "managed",
          wrapperPath: "/wrapper",
        },
      },
    };
    const store: MaterializedAppStore = {
      m_bIsInitialized: true,
      allApps: [],
      GetAppOverviewByAppID: () => null,
    };

    const shortcuts = discoverGames(configured, store).filter((game) => game.shortcut);
    expect(shortcuts).toEqual([
      expect.objectContaining({ appId: "43", enabled: true, missing: true }),
    ]);
  });
});

describe("launch option reading", () => {
  it("loads uncached shortcut details through Steam's details registration", async () => {
    const unregister = vi.fn();
    const bridge = bridgeWithoutOverview().bridge;
    const apps = bridge.Apps;
    bridge.Apps.RegisterForAppDetails = function (this: SteamBridge["Apps"], appId, callback) {
      expect(this).toBe(apps);
      expect(appId).toBe(2650715882);
      queueMicrotask(() => callback({ strShortcutLaunchOptions: "--original" }));
      return { unregister };
    };

    const value = await readLaunchValue(
      {
        appId: "2650715882",
        name: "Crimson Desert",
        shortcut: true,
        enabled: false,
        running: false,
      },
      bridge,
      { GetAppDetails: () => null },
    );

    expect(value).toBe("--original");
    expect(unregister).toHaveBeenCalledOnce();
  });
});

describe("disabling shortcut management", () => {
  const configuration: Configuration = {
    version: 1,
    games: {
      "42": {
        appId: "42",
        name: "Heroic Game",
        shortcut: true,
        originalLaunch: "original",
        managedLaunch: "managed",
        wrapperPath: "/wrapper",
      },
    },
  };

  it("preserves a recreated shortcut value and removes management", async () => {
    api.getConfiguration.mockResolvedValue(configuration);
    api.disableGame.mockResolvedValue(undefined);
    const fixture = bridgeWithoutOverview();

    await disableManagedGame(
      { appId: "42", name: "Heroic Game", shortcut: true, enabled: true, running: false },
      fixture.bridge,
      { GetAppDetails: () => ({ strShortcutLaunchOptions: "reinstalled game" }) },
    );

    expect(api.disableGame).toHaveBeenCalledWith("42");
    expect(fixture.bridge.Apps.SetShortcutLaunchOptions).not.toHaveBeenCalled();
  });

  it("restores the original value while the managed wrapper is current", async () => {
    api.getConfiguration.mockResolvedValue(configuration);
    api.disableGame.mockResolvedValue(undefined);
    const fixture = bridgeWithoutOverview();

    await disableManagedGame(
      { appId: "42", name: "Heroic Game", shortcut: true, enabled: true, running: false },
      fixture.bridge,
      { GetAppDetails: () => ({ strShortcutLaunchOptions: "managed" }) },
    );

    expect(fixture.bridge.Apps.SetShortcutLaunchOptions).toHaveBeenCalledWith(42, "original");
    expect(api.disableGame).toHaveBeenCalledWith("42");
  });
});

describe("changing a managed shortcut method", () => {
  const configuration: Configuration = {
    version: 1,
    games: {
      "42": {
        appId: "42",
        name: "Heroic Game",
        shortcut: true,
        originalLaunch: "original",
        managedLaunch: "managed-proton",
        wrapperPath: "/wrapper",
      },
    },
  };
  const game = { appId: "42", name: "Heroic Game", shortcut: true, enabled: true, running: false };

  it("passes the current Steam value to backend conflict validation and preserves an external edit", async () => {
    api.getConfiguration.mockResolvedValue(configuration);
    api.reconfigureGame.mockRejectedValue(new Error("Steam launch options changed outside HV Launcher"));
    const fixture = bridgeWithoutOverview();

    await expect(reconfigureManagedGame(
      game,
      "runtime",
      fixture.bridge,
      { GetAppDetails: () => ({ strShortcutLaunchOptions: "externally edited" }) },
    )).rejects.toThrow("changed outside HV Launcher");

    expect(api.reconfigureGame).toHaveBeenCalledWith("42", "externally edited", "runtime");
    expect(fixture.bridge.Apps.SetShortcutLaunchOptions).not.toHaveBeenCalled();
  });

  it("applies the backend-generated runtime launch value", async () => {
    api.getConfiguration.mockResolvedValue(configuration);
    api.reconfigureGame.mockResolvedValue({
      appId: "42",
      managedLaunch: "managed-runtime",
      wrapperPath: "/wrapper",
      mode: "runtime",
    });
    const fixture = bridgeWithoutOverview();

    await reconfigureManagedGame(
      game,
      "runtime",
      fixture.bridge,
      { GetAppDetails: () => ({ strShortcutLaunchOptions: "managed-proton" }) },
    );

    expect(fixture.bridge.Apps.SetShortcutLaunchOptions).toHaveBeenCalledWith(42, "managed-runtime");
  });

  it("restores the backend record if Steam rejects the new value", async () => {
    api.getConfiguration.mockResolvedValue(configuration);
    api.reconfigureGame
      .mockResolvedValueOnce({ appId: "42", managedLaunch: "managed-runtime", wrapperPath: "/wrapper", mode: "runtime" })
      .mockResolvedValueOnce({ appId: "42", managedLaunch: "managed-proton", wrapperPath: "/wrapper", mode: "proton" });
    const fixture = bridgeWithoutOverview();
    vi.mocked(fixture.bridge.Apps.SetShortcutLaunchOptions).mockImplementationOnce(() => {
      throw new Error("Steam setter failed");
    });

    await expect(reconfigureManagedGame(
      game,
      "runtime",
      fixture.bridge,
      { GetAppDetails: () => ({ strShortcutLaunchOptions: "managed-proton" }) },
    )).rejects.toThrow("Steam setter failed");

    expect(api.reconfigureGame).toHaveBeenNthCalledWith(1, "42", "managed-proton", "runtime");
    expect(api.reconfigureGame).toHaveBeenNthCalledWith(2, "42", "managed-runtime", "proton");
  });
});
