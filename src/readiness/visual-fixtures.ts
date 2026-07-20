import type { ProtonDraft, UMIPDraft } from '../readiness-workspace/readiness-workspace-state';
import type {
  AggregateStatus,
  Check,
  Configuration,
  ProtonInstallResult,
  ProtonSelectionResponse,
  SetupJobSnapshot,
  SystemStatus,
  UMIPCandidate,
  UMIPInspection
} from '../types';

type ProtonWorkspaceFixtureName = 'proton-missing' | 'proton-confirm' | 'proton-installing' | 'proton-success' | 'proton-failure';
type UMIPWorkspaceFixtureName =
  | 'umip-automatic'
  | 'umip-choice'
  | 'umip-manual'
  | 'umip-existing'
  | 'umip-success'
  | 'umip-failure';
type WorkspaceFixtureName = ProtonWorkspaceFixtureName | UMIPWorkspaceFixtureName;
export type VisualFixtureName = AggregateStatus | 'native-intel7' | 'z1-extreme' | 'z1-extreme-native' | WorkspaceFixtureName | '';
type QAMFixtureName = Exclude<VisualFixtureName, '' | WorkspaceFixtureName>;

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

const fixtures: Record<QAMFixtureName, QAMVisualFixture> = {
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
        check('proton', 'Proton', 'no supported build detected', false, 'Open Readiness details and setup to install a LinUwUx Proton archive.')
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
  if (!name) return undefined;
  if (name === 'proton-success') {
    const fixture = fixtures['hypervisor-ready'];
    const tools = ['GE-Proton11-1-LinUwUx', 'cachyos_11.0_20260702-LinUwUx'];
    return {
      ...fixture,
      status: {
        ...fixture.status,
        proton: { found: true, tools },
        checks: fixture.status.checks.map((item) =>
          item.id === 'proton' ? { ...item, detail: tools.join(', ') } : item
        )
      }
    };
  }
  if (name.startsWith('proton-') || name.startsWith('umip-')) return fixtures['setup-required'];
  return fixtures[name as QAMFixtureName];
}

const protonSelection: ProtonSelectionResponse = {
  selectionId: 'X3m5Qn8VisualSelection',
  expiresAt: '2026-07-18T12:10:00Z',
  responsibility: "HV Launcher cannot verify this archive's publisher, authenticity, or suitability. Confirm that you sourced and selected the intended archive before installing.",
  preflight: {
    fileName: 'cachyos-11.0-LinUwUx.tar.xz',
    compression: 'xz',
    compressedBytes: 1642824990,
    destinations: [
      { id: 'native', label: 'Steam (native)' },
      { id: 'flatpak', label: 'Steam (Flatpak)' }
    ]
  }
};

function protonJob(state: SetupJobSnapshot['state']): SetupJobSnapshot {
  return {
    id: 'VisualProtonJob',
    kind: 'proton-install',
    state,
    phase: state === 'running' ? 'installing' : state === 'succeeded' ? 'complete' : 'failed',
    progress: state === 'running' ? 58 : 100,
    output: state === 'running' ? ['Validating the selected Proton archive'] : [],
    error: state === 'failed' ? 'The destination became unavailable during installation.' : undefined,
    result: state === 'succeeded' ? {
      toolName: 'cachyos_11.0_20260702-LinUwUx',
      destinationId: 'native',
      sha256: 'f06a82e15cdd2b49fa8287bd9f8be4ea3d09a9f1cb5566339a3d61c38a5d902e',
      restartSteam: true
    } : undefined,
    startedAt: '2026-07-18T12:00:00Z',
    finishedAt: state === 'running' ? undefined : '2026-07-18T12:01:00Z'
  };
}

const completedProtonInstall: ProtonInstallResult = {
  toolName: 'cachyos_11.0_20260702-LinUwUx',
  destinationId: 'native',
  sha256: 'f06a82e15cdd2b49fa8287bd9f8be4ea3d09a9f1cb5566339a3d61c38a5d902e',
  restartSteam: true
};

export function getReadinessWorkspaceProtonFixture(
  name: VisualFixtureName = selectedFixture
): ProtonDraft | undefined {
  if (!name.startsWith('proton-')) return undefined;
  const reviewed: ProtonDraft = {
    stage: 'confirm',
    archivePath: '/home/deck/Downloads/cachyos-11.0-LinUwUx.tar.xz',
    selection: protonSelection,
    destinationId: undefined
  };
  switch (name) {
    case 'proton-missing':
      return { stage: 'idle' };
    case 'proton-confirm':
      return reviewed;
    case 'proton-installing':
      return { ...reviewed, stage: 'installing', job: protonJob('running') };
    case 'proton-success':
      return { stage: 'idle', lastInstall: completedProtonInstall };
    case 'proton-failure':
      return { ...reviewed, stage: 'failure', job: protonJob('failed'), error: protonJob('failed').error };
    default:
      return undefined;
  }
}

const limineCandidate: UMIPCandidate = {
  bootloader: 'limine',
  configuration: '/etc/default/limine',
  updater: { path: '/usr/bin/limine-update', args: [] },
  state: 'action-required',
  currentValue: 'quiet splash',
  proposedValue: 'quiet splash clearcpuid=514',
  detail: 'clearcpuid=514 can be added after review.'
};

const grubCandidate: UMIPCandidate = {
  bootloader: 'grub',
  configuration: '/etc/default/grub',
  updater: { path: '/usr/bin/grub-mkconfig', args: ['-o', '/boot/grub/grub.cfg'] },
  state: 'action-required',
  currentValue: 'quiet splash',
  proposedValue: 'quiet splash clearcpuid=514',
  detail: 'clearcpuid=514 can be added after review.'
};

const automaticUMIPInspection: UMIPInspection = {
  liveUmip: true,
  selection: 'automatic',
  selected: 'limine',
  candidates: [limineCandidate],
  manual: []
};

function umipJob(state: SetupJobSnapshot['state']): SetupJobSnapshot {
  return {
    id: 'VisualUMIPJob',
    kind: 'umip-apply',
    state,
    phase: state === 'running' ? 'regenerating-boot-configuration' : state === 'succeeded' ? 'complete' : 'failed',
    progress: state === 'running' ? 65 : 100,
    output: state === 'running' ? ['Updating bootloader configuration'] : [],
    error: state === 'failed' ? 'The bootloader updater failed and the original configuration was restored.' : undefined,
    result: state === 'succeeded' ? { bootloader: 'limine', restartRequired: true } : undefined,
    startedAt: '2026-07-19T12:00:00Z',
    finishedAt: state === 'running' ? undefined : '2026-07-19T12:00:04Z'
  };
}

export function getReadinessWorkspaceUMIPFixture(
  name: VisualFixtureName = selectedFixture
): UMIPDraft | undefined {
  if (!name) return undefined;
  if (!name.startsWith('umip-')) {
    const visualStatus = getQAMVisualFixture(name)?.status;
    if (!visualStatus?.checks.some((item) => item.id === 'umip')) return undefined;
    return visualStatus.cpu.umipPresent
      ? { stage: 'idle', inspection: automaticUMIPInspection, selected: 'limine' }
      : {
          stage: 'idle',
          inspection: { liveUmip: false, selection: 'manual-only', candidates: [], manual: [] }
        };
  }

  switch (name) {
    case 'umip-automatic':
      return { stage: 'idle', inspection: automaticUMIPInspection, selected: 'limine' };
    case 'umip-choice':
      return {
        stage: 'idle',
        inspection: {
          liveUmip: true,
          selection: 'choice-required',
          candidates: [limineCandidate, grubCandidate],
          manual: []
        }
      };
    case 'umip-manual':
      return {
        stage: 'idle',
        inspection: {
          liveUmip: true,
          selection: 'manual-only',
          candidates: [],
          manual: [{
            reason: 'unsupported-bootloader',
            detail: 'No supported Limine or GRUB configuration was found; add clearcpuid=514 manually.'
          }]
        }
      };
    case 'umip-existing': {
      const configured: UMIPCandidate = {
        ...limineCandidate,
        state: 'restart-required',
        existingArgument: 'clearcpuid=514',
        detail: 'The UMIP argument is already configured; restart the system to apply it.'
      };
      return {
        stage: 'restart-required',
        inspection: { ...automaticUMIPInspection, candidates: [configured] },
        selected: 'limine'
      };
    }
    case 'umip-success':
      return {
        stage: 'restart-required',
        inspection: automaticUMIPInspection,
        selected: 'limine',
        job: umipJob('succeeded')
      };
    case 'umip-failure':
      return {
        stage: 'failure',
        inspection: automaticUMIPInspection,
        selected: 'limine',
        job: umipJob('failed'),
        error: umipJob('failed').error
      };
    default:
      return undefined;
  }
}
