import { describe, expect, it } from 'vitest';
import {
  getQAMVisualFixture,
  getReadinessWorkspaceModuleFixture,
  getReadinessWorkspaceProtonFixture,
  getReadinessWorkspaceUMIPFixture,
  type VisualFixtureName
} from './visual-fixtures';

const fixtures: Array<[Exclude<VisualFixtureName, ''>, string]> = [
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
  it.each(fixtures)('provides a coherent %s payload', (name, expectedStatus) => {
    const fixture = getQAMVisualFixture(name);

    expect(fixture?.status.status).toBe(expectedStatus);
    expect(fixture?.status.checks.length).toBeGreaterThan(0);
    expect(fixture?.status.checks.every((check) => check.label && check.detail)).toBe(true);
  });

  it('is disabled without a selected fixture', () => {
    expect(getQAMVisualFixture('')).toBeUndefined();
  });

  it('includes passing and failing rows in the setup fixture', () => {
    const checks = getQAMVisualFixture('setup-required')?.status.checks ?? [];

    expect(checks.some((check) => check.ok)).toBe(true);
    expect(checks.some((check) => !check.ok && check.remedy)).toBe(true);
  });

  it('models the Intel 7th generation visual case', () => {
    const fixture = getQAMVisualFixture('native-intel7')?.status;

    expect(fixture?.cpu.generation).toBe('Intel 7th generation');
    expect(fixture?.kernel.release).toBe('6.0.1-visual-fixture');
    expect(fixture?.path).toBe('native');
  });

  it('models the pre-6.18 Z1 Extreme hypervisor case', () => {
    const fixture = getQAMVisualFixture('z1-extreme')?.status;

    expect(fixture?.cpu.generation).toBe('AMD Zen 4');
    expect(fixture?.kernel.minor).toBeLessThan(18);
    expect(fixture?.path).toBe('hypervisor');
  });

  it('models the kernel 6.18 Z1 Extreme native case', () => {
    const fixture = getQAMVisualFixture('z1-extreme-native')?.status;

    expect(fixture?.cpu.cpuidFaultFlag).toBe(true);
    expect(fixture?.kernel.minor).toBe(18);
    expect(fixture?.path).toBe('native');
  });

  it.each([
    ['proton-missing', 'idle'],
    ['proton-confirm', 'confirm'],
    ['proton-installing', 'installing'],
    ['proton-failure', 'failure']
  ] as const)('models the %s readiness-workspace process', (name, stage) => {
    const draft = getReadinessWorkspaceProtonFixture(name);
    expect(getQAMVisualFixture(name)?.status.status).toBe('setup-required');
    expect(draft?.stage).toBe(stage);
  });

  it('models completed Proton setup as reusable management with installed builds', () => {
    const fixture = getQAMVisualFixture('proton-success');
    const draft = getReadinessWorkspaceProtonFixture('proton-success');

    expect(fixture?.status.status).toBe('hypervisor-ready');
    expect(fixture?.status.proton.tools).toEqual([
      'GE-Proton11-1-LinUwUx',
      'cachyos_11.0_20260702-LinUwUx'
    ]);
    expect(draft?.stage).toBe('idle');
    expect(draft?.lastInstall?.toolName).toBe('cachyos_11.0_20260702-LinUwUx');
  });

  it.each([
    ['umip-automatic', 'idle'],
    ['umip-choice', 'idle'],
    ['umip-manual', 'idle'],
    ['umip-existing', 'restart-required'],
    ['umip-success', 'restart-required'],
    ['umip-failure', 'failure']
  ] as const)('models the %s readiness-workspace process', (name, stage) => {
    const draft = getReadinessWorkspaceUMIPFixture(name);

    expect(getQAMVisualFixture(name)?.status.cpu.umipPresent).toBe(true);
    expect(draft?.stage).toBe(stage);
  });

  it('models explicit dual-bootloader choice and manual-only guidance', () => {
    const choice = getReadinessWorkspaceUMIPFixture('umip-choice');
    const manual = getReadinessWorkspaceUMIPFixture('umip-manual');

    expect(choice?.inspection?.selection).toBe('choice-required');
    expect(choice?.inspection?.candidates.map((candidate) => candidate.bootloader)).toEqual(['limine', 'grub']);
    expect(manual?.inspection?.selection).toBe('manual-only');
    expect(manual?.inspection?.manual[0].detail).toContain('manually');
  });

  it.each([
    ['module-missing', 'idle'],
    ['module-ready', 'idle'],
    ['module-review', 'review'],
    ['module-installing', 'installing'],
    ['module-success', 'complete'],
    ['module-signing', 'complete'],
    ['module-failure', 'failure'],
    ['module-conflict', 'failure'],
    ['module-manual', 'idle']
  ] as const)('models the %s CPUID-module process', (name, stage) => {
    const fixture = getReadinessWorkspaceModuleFixture(name);

    expect(getQAMVisualFixture(name)?.status.status).toBe('setup-required');
    expect(fixture?.draft.stage).toBe(stage);
  });

  it('models package review, immutable manual guidance, and signing follow-up', () => {
    const review = getReadinessWorkspaceModuleFixture('module-review');
    const manual = getReadinessWorkspaceModuleFixture('module-manual');
    const signing = getReadinessWorkspaceModuleFixture('module-signing');

    expect(review?.preflight.dependencyPlan?.packages).toContain('dkms');
    expect(getReadinessWorkspaceModuleFixture('module-ready')?.preflight.ready).toBe(true);
    expect(manual?.preflight.dependencyPlan).toBeUndefined();
    expect(manual?.preflight.dependencyPlanError).toContain('manual');
    expect(signing?.draft.result?.signingRequired).toBe(true);
  });
});
