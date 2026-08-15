import { describe, expect, it } from 'vitest';
import {
  getQAMVisualFixture,
  getReadinessWorkspaceModuleFixture,
  getReadinessWorkspaceProtonFixture,
  getReadinessWorkspaceUMIPFixture,
  type VisualFixtureName
} from '@/readiness/visual-fixtures';

const qamNames: ReadonlyArray<[VisualFixtureName, string]> = [
  ['native-ready', 'native-ready'],
  ['native-intel7', 'native-ready'],
  ['z1-extreme', 'hypervisor-ready'],
  ['z1-extreme-native', 'native-ready'],
  ['hypervisor-ready', 'hypervisor-ready'],
  ['setup-required', 'setup-required'],
  ['recovery-required', 'recovery-required'],
  ['unsupported', 'unsupported']
];

describe('QAM visual fixtures', () => {
  it('keeps every named payload coherent', () => {
    for (const [name, expectedStatus] of qamNames) {
      const fixture = getQAMVisualFixture(name);
      expect(fixture?.status.status).toBe(expectedStatus);
      expect(fixture?.status.checks.length).toBeGreaterThan(0);
      expect(fixture?.status.checks.every((check) => check.label && check.detail)).toBe(true);
    }
  });

  it('routes workspace names to the setup-required payload and models the completed Proton state', () => {
    for (const name of ['proton-confirm', 'umip-choice', 'module-review'] as const) {
      expect(getQAMVisualFixture(name)?.status.status).toBe('setup-required');
    }
    const success = getQAMVisualFixture('proton-success');
    expect(success?.status.status).toBe('hypervisor-ready');
    expect(success?.status.proton.tools).toEqual([
      'GE-Proton11-1-LinUwUx',
      'cachyos_11.0_20260702-LinUwUx'
    ]);
  });

  it('refuses an empty selection', () => {
    expect(getQAMVisualFixture('')).toBeUndefined();
  });
});

describe('readiness workspace fixture guards', () => {
  it('rejects workspace names from the wrong domain', () => {
    expect(getReadinessWorkspaceProtonFixture('module-review')).toBeUndefined();
    expect(getReadinessWorkspaceModuleFixture('proton-confirm')).toBeUndefined();
  });

  it('derives a UMIP draft from statuses exposing a UMIP check', () => {
    expect(getReadinessWorkspaceUMIPFixture('setup-required')?.inspection).toBeDefined();
    expect(getReadinessWorkspaceUMIPFixture('')).toBeUndefined();
  });
});
