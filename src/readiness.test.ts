import { describe, expect, it } from 'vitest';
import {
  aggregateReadinessState,
  kvmReadinessState,
  managerReadinessState,
  pathReadinessState
} from './readiness';
import type { SystemStatus } from './types';

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
  it('treats both usable paths as successful aggregate states', () => {
    expect(aggregateReadinessState('native-ready')).toBe('success');
    expect(aggregateReadinessState('hypervisor-ready')).toBe('success');
    expect(aggregateReadinessState('setup-required')).toBe('warning');
    expect(aggregateReadinessState('recovery-required')).toBe('error');
    expect(aggregateReadinessState('unsupported')).toBe('error');
  });

  it('marks compatible methods ready and reserves errors for no method', () => {
    expect(pathReadinessState('native')).toBe('success');
    expect(pathReadinessState('hypervisor')).toBe('success');
    expect(pathReadinessState('none')).toBe('error');
  });

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
