import { describe, expect, it, vi } from 'vitest';
import {
  discoverGames,
  observeSteamLifetime,
  SteamLibraryLoadingError,
  type MaterializedAppStore,
  type SteamBridge
} from '@/steam';
import type { Configuration } from '@/types';

function createSteamBridgeFixture() {
  let lifetime: ((notification: { unAppID: number; nInstanceID: number; bRunning: boolean }) => void) | undefined;
  const unregister = vi.fn();
  const registerLifetime = vi.fn((callback: NonNullable<typeof lifetime>) => {
    lifetime = callback;
    return { unregister };
  });
  const bridge: SteamBridge = {
    Apps: {
      SetAppLaunchOptions: vi.fn(),
      SetShortcutLaunchOptions: vi.fn()
    },
    GameSessions: {
      RegisterForAppLifetimeNotifications: registerLifetime
    }
  };
  return {
    bridge,
    emit: (value: Parameters<NonNullable<typeof lifetime>>[0]) => lifetime?.(value),
    registerLifetime,
    unregister
  };
}

describe('Steam observation', () => {
  it('retries unresolved App ID zero and cancels cleanly', async () => {
    vi.useFakeTimers();
    const fixture = createSteamBridgeFixture();
    const sendLifetime = vi.fn(async () => ({ status: 'unresolved' }));
    const cleanup = observeSteamLifetime({ bridge: fixture.bridge, sendLifetime });
    fixture.emit({ unAppID: 0, nInstanceID: 7, bRunning: true });
    await vi.runAllTimersAsync();
    expect(sendLifetime).toHaveBeenCalledTimes(4);
    cleanup();
    vi.useRealTimers();
  });

  it('reports lifetime forwarding failures without an unhandled rejection', async () => {
    const fixture = createSteamBridgeFixture();
    const onError = vi.fn();
    const cleanup = observeSteamLifetime({
      bridge: fixture.bridge,
      onError,
      sendLifetime: vi.fn(async () => {
        throw new Error('backend unavailable');
      })
    });
    fixture.emit({ unAppID: 10, nInstanceID: 22, bRunning: true });
    await vi.waitFor(() =>
      expect(onError).toHaveBeenCalledWith(expect.objectContaining({ message: 'backend unavailable' }))
    );
    cleanup();
  });
});

describe('allApps discovery', () => {
  const configuration: Configuration = {
    version: 1,
    games: {
      '42': {
        appId: '42',
        name: 'Stale Game',
        shortcut: false,
        originalLaunch: '',
        managedLaunch: 'managed',
        wrapperPath: '/wrapper'
      }
    }
  };

  it('uses initialized allApps and preserves unsigned shortcut IDs', () => {
    const store: MaterializedAppStore = {
      m_bIsInitialized: true,
      allApps: [
        {
          appid: 2650715882,
          display_name: 'Crimson Desert',
          app_type: 0x40000000,
          visible_in_game_list: true,
          per_client_data: [{ installed: true, display_status: 11 }]
        },
        {
          appid: 10,
          display_name: 'Steam Game',
          app_type: 1,
          visible_in_game_list: true,
          per_client_data: [{ installed: true, display_status: 4 }]
        },
        {
          appid: 20,
          display_name: 'Uninstalled',
          app_type: 1,
          visible_in_game_list: true,
          per_client_data: [{ installed: false }]
        }
      ],
      GetAppOverviewByAppID: () => null
    };
    const games = discoverGames(configuration, store);
    expect(games.map((game) => game.appId).sort()).toEqual(['10', '2650715882', '42']);
    expect(games.find((game) => game.appId === '2650715882')?.shortcut).toBe(true);
    expect(games.find((game) => game.appId === '42')?.missing).toBe(true);
  });

  it('reports loading without another discovery fallback', () => {
    expect(() =>
      discoverGames(configuration, {
        m_bIsInitialized: false,
        allApps: [],
        GetAppOverviewByAppID: () => null
      })
    ).toThrow(SteamLibraryLoadingError);
    expect(() =>
      discoverGames(configuration, {
        m_bIsInitialized: true,
        GetAppOverviewByAppID: () => null
      })
    ).toThrow(SteamLibraryLoadingError);
  });

  it('reports loading when Decky app stores are unavailable', () => {
    expect(() => discoverGames(configuration)).toThrow(SteamLibraryLoadingError);
  });

  it('preserves a configured missing shortcut for restoration while excluding stale native apps', () => {
    const configured: Configuration = {
      version: 1,
      games: {
        '42': configuration.games['42'],
        '43': {
          appId: '43',
          name: 'Missing Heroic Shortcut',
          shortcut: true,
          originalLaunch: '',
          managedLaunch: 'managed',
          wrapperPath: '/wrapper'
        }
      }
    };
    const store: MaterializedAppStore = {
      m_bIsInitialized: true,
      allApps: [],
      GetAppOverviewByAppID: () => null
    };

    const shortcuts = discoverGames(configured, store).filter((game) => game.shortcut);
    expect(shortcuts).toEqual([expect.objectContaining({ appId: '43', enabled: true, missing: true })]);
  });
});
