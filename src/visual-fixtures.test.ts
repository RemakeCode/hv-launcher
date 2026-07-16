import { describe, expect, it } from 'vitest';
import { getQAMVisualFixture, type VisualFixtureName } from './visual-fixtures';

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
});
