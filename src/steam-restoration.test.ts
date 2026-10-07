import { beforeEach, describe, expect, it, vi } from 'vitest';
import { disableGame, getConfiguration } from '@/api';
import { disableManagedGame } from '@/steam';
import type { SteamBridge } from '@/steam';
import type { Configuration, Game } from '@/types';

vi.mock('@/api', () => ({
  disableGame: vi.fn(),
  enableGame: vi.fn(),
  getConfiguration: vi.fn(),
  postLifetime: vi.fn()
}));

const game: Game = { appId: '42', name: 'Example', shortcut: true, enabled: true };

function restorationFixture(originalLaunch = 'original options') {
  const configuration: Configuration = {
    version: 1,
    games: {
      '42': {
        appId: '42',
        name: game.name,
        shortcut: true,
        originalLaunch,
        managedLaunch: 'managed options',
        wrapperPath: '/wrapper',
        mode: 'runtime'
      }
    }
  };
  const bridge: SteamBridge = {
    Apps: {
      SetAppLaunchOptions: vi.fn(),
      SetShortcutLaunchOptions: vi.fn(),
      RegisterForAppDetails: vi.fn((_appId, callback) => {
        callback({ strShortcutLaunchOptions: 'broken user edit' });
      })
    },
    GameSessions: { RegisterForAppLifetimeNotifications: vi.fn() }
  };
  vi.mocked(getConfiguration).mockResolvedValue(configuration);
  vi.mocked(disableGame).mockResolvedValue(undefined);
  return bridge;
}

describe('shortcut restoration', () => {
  beforeEach(() => vi.resetAllMocks());

  it('restores original options without inspecting the current launch command', async () => {
    const bridge = restorationFixture();

    await disableManagedGame(game, bridge);

    expect(bridge.Apps.RegisterForAppDetails).not.toHaveBeenCalled();
    expect(bridge.Apps.SetShortcutLaunchOptions).toHaveBeenCalledExactlyOnceWith(42, 'original options');
    expect(disableGame).toHaveBeenCalledExactlyOnceWith('42');
    expect(vi.mocked(bridge.Apps.SetShortcutLaunchOptions).mock.invocationCallOrder[0]).toBeLessThan(
      vi.mocked(disableGame).mock.invocationCallOrder[0]
    );
  });

  it('restores empty original options', async () => {
    const bridge = restorationFixture('');

    await disableManagedGame(game, bridge);

    expect(bridge.Apps.SetShortcutLaunchOptions).toHaveBeenCalledExactlyOnceWith(42, '');
  });

  it('rolls back to managed options when backend removal fails', async () => {
    const bridge = restorationFixture();
    vi.mocked(disableGame).mockRejectedValue(new Error('removal failed'));

    await expect(disableManagedGame(game, bridge)).rejects.toThrow('removal failed');

    expect(bridge.Apps.SetShortcutLaunchOptions).toHaveBeenNthCalledWith(1, 42, 'original options');
    expect(bridge.Apps.SetShortcutLaunchOptions).toHaveBeenNthCalledWith(2, 42, 'managed options');
  });

  it('keeps the managed record when Steam cannot restore the original options', async () => {
    const bridge = restorationFixture();
    vi.mocked(bridge.Apps.SetShortcutLaunchOptions).mockImplementation(() => {
      throw new Error('Steam unavailable');
    });

    await expect(disableManagedGame(game, bridge)).rejects.toThrow('Steam unavailable');

    expect(disableGame).not.toHaveBeenCalled();
  });
});
