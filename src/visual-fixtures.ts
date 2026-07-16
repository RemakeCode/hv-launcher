import type { AggregateStatus, Check, Configuration, SystemStatus } from './types';

export type VisualFixtureName = AggregateStatus | 'native-intel7' | 'z1-extreme' | 'z1-extreme-native' | '';

export interface QAMVisualFixture {
  configuration: Configuration;
  status: SystemStatus;
}

const embeddedFixture: string = '__HV_QAM_VISUAL_FIXTURE__';
const selectedFixture = embeddedFixture.startsWith('__HV_')
  ? ''
  : embeddedFixture as VisualFixtureName;

const emptyConfiguration: Configuration = { version: 1, games: {} };

const managedConfiguration: Configuration = {
  version: 1,
  games: {
    '1234567890': {
      appId: '1234567890',
      name: 'Visual fixture shortcut',
      shortcut: true,
      originalLaunch: 'run-game',
      managedLaunch: 'hv-launcher-wrapper -- run-game',
      wrapperPath: '/tmp/hv-launcher-wrapper'
    }
  }
};

function check(id: string, label: string, detail: string, ok = true, remedy?: string): Check {
  return { id, label, detail, ok, remedy };
}

function status(overrides: Partial<SystemStatus>): SystemStatus {
  return {
    status: 'hypervisor-ready',
    path: 'hypervisor',
    cpu: {
      vendor: 'AuthenticAMD',
      modelName: 'AMD Ryzen 7 5700X 8-Core Processor',
      family: 25,
      modelId: 33,
      architecture: 'zen3',
      generation: 'AMD Zen 3',
      supported: true,
      steamDeck: false,
      umipPresent: false,
      umipRequiredOff: true,
      cpuidFaultFlag: false
    },
    kernel: { release: '6.14.0-visual-fixture', major: 6, minor: 14, supported: true },
    modules: {
      emulationInstalled: true,
      emulationLoaded: false,
      emulationCompatible: true,
      kvmLoaded: true,
      kvmAmdLoaded: true,
      kvmBusy: false,
      controllerState: 'idle'
    },
    proton: { found: true, tools: ['GE-Proton11-1-LinUwUx'] },
    checks: [],
    ...overrides
  };
}

const fixtures: Record<Exclude<VisualFixtureName, ''>, QAMVisualFixture> = {
  'native-ready': {
    configuration: emptyConfiguration,
    status: status({
      status: 'native-ready',
      path: 'native',
      kernel: { release: '6.18.0-visual-fixture', major: 6, minor: 18, supported: true },
      cpu: {
        vendor: 'AuthenticAMD',
        modelName: 'AMD Ryzen 9 9950X3D 16-Core Processor',
        family: 26,
        modelId: 68,
        architecture: 'zen5',
        generation: 'AMD Zen 5',
        supported: true,
        steamDeck: false,
        umipPresent: false,
        umipRequiredOff: true,
        cpuidFaultFlag: true
      },
      checks: [
        check('cpu', 'CPU', 'AMD Zen 5'),
        check('kernel', 'Linux kernel', '6.18.0-visual-fixture'),
        check('umip', 'UMIP', 'disabled as required'),
        check('cpuid-fault', 'Native CPUID faulting', 'advertised by the running kernel'),
        check('proton', 'Proton', 'GE-Proton11-1-LinUwUx')
      ]
    })
  },
  'native-intel7': {
    configuration: emptyConfiguration,
    status: status({
      status: 'native-ready',
      path: 'native',
      kernel: { release: '6.0.1-visual-fixture', major: 6, minor: 0, supported: true },
      cpu: {
        vendor: 'GenuineIntel',
        modelName: 'Intel(R) Core(TM) i7-7500U CPU @ 2.70GHz',
        family: 6,
        modelId: 142,
        architecture: 'intel-gen7',
        generation: 'Intel 7th generation',
        supported: true,
        steamDeck: false,
        umipPresent: false,
        umipRequiredOff: false,
        cpuidFaultFlag: true
      },
      modules: {
        emulationInstalled: false,
        emulationLoaded: false,
        emulationCompatible: false,
        kvmLoaded: true,
        kvmAmdLoaded: false,
        kvmBusy: false,
        controllerState: 'idle'
      },
      checks: [
        check('cpu', 'CPU', 'Intel 7th generation'),
        check('kernel', 'Linux kernel', '6.0.1-visual-fixture'),
        check('umip', 'UMIP', 'not required'),
        check('cpuid-fault', 'Native CPUID faulting', 'advertised by the running kernel'),
        check('proton', 'Proton', 'GE-Proton11-1-LinUwUx')
      ]
    })
  },
  'z1-extreme': {
    configuration: emptyConfiguration,
    status: status({
      status: 'hypervisor-ready',
      path: 'hypervisor',
      kernel: { release: '6.11.11-visual-fixture', major: 6, minor: 11, supported: true },
      cpu: {
        vendor: 'AuthenticAMD',
        modelName: 'AMD Ryzen Z1 Extreme w/ Radeon 780M Graphics',
        family: 25,
        modelId: 116,
        architecture: 'zen4',
        generation: 'AMD Zen 4',
        supported: true,
        steamDeck: false,
        umipPresent: false,
        umipRequiredOff: true,
        cpuidFaultFlag: false
      },
      checks: [
        check('cpu', 'CPU', 'AMD Zen 4'),
        check('kernel', 'Linux kernel', '6.11.11-visual-fixture'),
        check('umip', 'UMIP', 'disabled as required'),
        check('emulation-module', 'CPUID module', 'installed and compatible'),
        check('proton', 'Proton', 'GE-Proton11-1-LinUwUx')
      ]
    })
  },
  'z1-extreme-native': {
    configuration: emptyConfiguration,
    status: status({
      status: 'native-ready',
      path: 'native',
      kernel: { release: '6.18.0-visual-fixture', major: 6, minor: 18, supported: true },
      cpu: {
        vendor: 'AuthenticAMD',
        modelName: 'AMD Ryzen Z1 Extreme w/ Radeon 780M Graphics',
        family: 25,
        modelId: 116,
        architecture: 'zen4',
        generation: 'AMD Zen 4',
        supported: true,
        steamDeck: false,
        umipPresent: false,
        umipRequiredOff: true,
        cpuidFaultFlag: true
      },
      checks: [
        check('cpu', 'CPU', 'AMD Zen 4'),
        check('kernel', 'Linux kernel', '6.18.0-visual-fixture'),
        check('umip', 'UMIP', 'disabled as required'),
        check('cpuid-fault', 'Native CPUID faulting', 'advertised by the running kernel'),
        check('proton', 'Proton', 'GE-Proton11-1-LinUwUx')
      ]
    })
  },
  'hypervisor-ready': {
    configuration: emptyConfiguration,
    status: status({
      checks: [
        check('cpu', 'CPU', 'AMD Zen 3'),
        check('kernel', 'Linux kernel', '6.14.0-visual-fixture'),
        check('umip', 'UMIP', 'disabled as required'),
        check('emulation-module', 'CPUID module', 'installed and compatible'),
        check('proton', 'Proton', 'GE-Proton11-1-LinUwUx')
      ]
    })
  },
  'setup-required': {
    configuration: emptyConfiguration,
    status: status({
      status: 'setup-required',
      cpu: {
        vendor: 'AuthenticAMD',
        modelName: 'AMD Ryzen 7 5700G with Radeon Graphics',
        family: 25,
        modelId: 80,
        architecture: 'zen3',
        generation: 'AMD Zen 3',
        supported: true,
        steamDeck: false,
        umipPresent: true,
        umipRequiredOff: true,
        cpuidFaultFlag: false
      },
      modules: {
        emulationInstalled: true,
        emulationLoaded: false,
        emulationCompatible: false,
        kvmLoaded: false,
        kvmAmdLoaded: false,
        kvmBusy: false,
        controllerState: 'idle'
      },
      proton: { found: false, tools: [] },
      checks: [
        check('cpu', 'CPU', 'AMD Zen 3'),
        check('kernel', 'Linux kernel', '6.14.0-visual-fixture'),
        check('umip', 'UMIP', 'enabled and blocking', false, 'Add clearcpuid=514 (or clearcpuid=umip) to the kernel command line and reboot.'),
        check('emulation-module', 'CPUID module', 'installed module does not match the running kernel', false, 'Install cpuid_fault_emulation through DKMS for the running kernel; the plugin does not install it.'),
        check('proton', 'Proton', 'no supported build detected', false, "Manually extract a supported LinUwUx Proton build into Steam's compatibilitytools.d directory.")
      ]
    })
  },
  'recovery-required': {
    configuration: managedConfiguration,
    status: status({
      status: 'recovery-required',
      modules: {
        emulationInstalled: true,
        emulationLoaded: true,
        emulationCompatible: true,
        kvmLoaded: false,
        kvmAmdLoaded: false,
        kvmBusy: false,
        controllerState: 'recovery-required'
      },
      checks: [
        check('cpu', 'CPU', 'AMD Zen 3'),
        check('kernel', 'Linux kernel', '6.14.0-visual-fixture'),
        check('umip', 'UMIP', 'disabled as required'),
        check('emulation-module', 'CPUID module', 'installed, compatible, and loaded'),
        check('proton', 'Proton', 'GE-Proton11-1-LinUwUx')
      ]
    })
  },
  unsupported: {
    configuration: emptyConfiguration,
    status: status({
      status: 'unsupported',
      path: 'none',
      cpu: {
        vendor: 'GenuineIntel',
        modelName: 'Intel(R) Core(TM) i7-3770 CPU @ 3.40GHz',
        family: 6,
        modelId: 58,
        architecture: 'intel-gen3',
        generation: 'Intel 3rd generation',
        supported: false,
        steamDeck: false,
        umipPresent: false,
        umipRequiredOff: false,
        cpuidFaultFlag: false
      },
      checks: [
        check('cpu', 'CPU', 'Intel 3rd generation', false, 'Requires Intel 4th generation or AMD Ryzen 1st generation or newer.'),
        check('kernel', 'Linux kernel', '6.14.0-visual-fixture'),
        check('umip', 'UMIP', 'not required'),
        check('proton', 'Proton', 'GE-Proton11-1-LinUwUx')
      ]
    })
  }
};

export function getQAMVisualFixture(name: VisualFixtureName = selectedFixture): QAMVisualFixture | undefined {
  return name ? fixtures[name] : undefined;
}
