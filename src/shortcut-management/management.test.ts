import { describe, expect, it } from 'vitest';
import {
  availableModes,
  effectiveGameMode,
  shortcuts,
  isModeAvailable,
  shouldShowShortcutManagement
} from '@/shortcut-management/management';
import type { Configuration, Game, SystemStatus } from '@/types';

const emptyConfiguration: Configuration = { version: 1, games: {} };
const managedConfiguration: Configuration = {
  version: 1,
  games: {
    '12': {
      appId: '12',
      name: 'Heroic Game',
      shortcut: true,
      originalLaunch: '',
      managedLaunch: 'managed',
      wrapperPath: '/wrapper'
    }
  }
};

function game(overrides: Partial<Game>): Game {
  return {
    appId: '1',
    name: 'Shortcut',
    shortcut: true,
    enabled: false,
    ...overrides
  };
}

describe('Shortcut management model', () => {
  it('filters native apps and groups shortcuts deterministically without a search model', () => {
    const sections = shortcuts([
      game({ appId: '3', name: 'Zulu', enabled: false }),
      game({ appId: '2', name: 'Alpha', enabled: true }),
      game({ appId: '4', name: 'Beta', enabled: false }),
      game({ appId: '1', name: 'Native game', shortcut: false, enabled: true })
    ]);

    expect(Object.keys(sections)).toEqual(['managed', 'available']);
    expect(sections.managed.map(({ name }) => name)).toEqual(['Alpha']);
    expect(sections.available.map(({ name }) => name)).toEqual(['Beta', 'Zulu']);
  });

  it('shows management for existing managed shortcuts on non-hypervisor paths', () => {
    expect(shouldShowShortcutManagement({ path: 'none' }, managedConfiguration)).toBe(true);
    expect(shouldShowShortcutManagement({ path: 'none' }, emptyConfiguration)).toBe(false);
  });

  it('offers management on the hypervisor path and for existing managed shortcuts', () => {
    expect(shouldShowShortcutManagement({ path: 'hypervisor' }, emptyConfiguration)).toBe(true);
    expect(shouldShowShortcutManagement({ path: 'native' }, emptyConfiguration)).toBe(false);
  });

  it('offers management on a native path when the runtime is available', () => {
    expect(shouldShowShortcutManagement(methodStatus(false, true), emptyConfiguration)).toBe(true);
  });

  it('exposes only installed method choices', () => {
    expect(effectiveGameMode(managedConfiguration, '12')).toBe('proton');
    expect(availableModes(methodStatus(true, false))).toEqual(['proton']);
    expect(availableModes(methodStatus(false, true))).toEqual(['runtime']);
    expect(availableModes(methodStatus(true, true))).toEqual(['proton', 'runtime']);
  });

  it('keeps Proton management available with an older backend status response', () => {
    const status = methodStatus(true, false);
    delete status.linuwux;

    expect(isModeAvailable(status, 'proton')).toBe(true);
    expect(isModeAvailable(status, 'runtime')).toBe(false);
    expect(availableModes(status)).toEqual(['proton']);
    expect(shouldShowShortcutManagement(status, managedConfiguration)).toBe(true);

    status.proton.found = false;
    expect(availableModes(status)).toEqual([]);
  });

  it('does not expose management for stale native-app records', () => {
    const configuration: Configuration = {
      version: 1,
      games: {
        '7': { ...managedConfiguration.games['12'], appId: '7', shortcut: false }
      }
    };
    expect(shouldShowShortcutManagement({ path: 'none' }, configuration)).toBe(false);
  });
});

function methodStatus(proton: boolean, runtime: boolean): SystemStatus {
  return {
    status: 'native-ready',
    path: 'native',
    cpu: {
      vendor: 'GenuineIntel',
      modelName: 'CPU',
      family: 6,
      modelId: 60,
      architecture: 'intel-gen4',
      generation: 'Intel 4th generation',
      supported: true,
      steamDeck: false,
      umipPresent: false,
      umipRequiredOff: false,
      cpuidFaultFlag: true
    },
    kernel: { release: '6.18', major: 6, minor: 18, supported: true },
    modules: {
      emulationInstalled: false,
      emulationLoaded: false,
      emulationCompatible: false,
      kvmLoaded: false,
      kvmAmdLoaded: false,
      kvmBusy: false,
      controllerState: 'idle'
    },
    proton: { found: proton, tools: proton ? ['LinUwUx Proton'] : [] },
    linuwux: {
      available: proton || runtime,
      proton: { found: proton, tools: proton ? ['LinUwUx Proton'] : [] },
      runtime: {
        supported: true,
        available: runtime,
        state: runtime ? 'available' : 'absent',
        path: '/home/deck/.local/bin/linuwux',
        libraryPath: '/home/deck/.local/share/linuwux/LinUwUx.so'
      }
    },
    checks: []
  };
}
