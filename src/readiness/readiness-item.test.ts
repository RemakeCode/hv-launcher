import { describe, expect, it } from 'vitest';
import {
  kvmReadinessState,
  managerReadinessState
} from '@/readiness/readiness-item';
import type { SystemStatus } from '@/types';

function modules(overrides: Partial<SystemStatus['modules']> = {}): SystemStatus['modules'] {
  return {
    emulationInstalled: true,
    emulationLoaded: false,
    emulationCompatible: true,
    kvmLoaded: false,
    kvmAmdLoaded: false,
    kvmBusy: false,
    controllerState: 'idle',
    ...overrides
  };
}

describe('readiness presentation states', () => {
  it('represents KVM and manager transitions without treating them as toggles', () => {
    expect(kvmReadinessState(modules({ kvmLoaded: true }))).toBe('success');
    expect(kvmReadinessState(modules())).toBe('success');
    expect(kvmReadinessState(modules({ controllerState: 'active' }))).toBe('active');
    expect(kvmReadinessState(modules({ kvmBusy: true }))).toBe('error');
    expect(managerReadinessState('idle')).toBe('success');
    expect(managerReadinessState('activating')).toBe('active');
    expect(managerReadinessState('recovery-required')).toBe('error');
  });
});
