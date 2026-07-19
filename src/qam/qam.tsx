import { ButtonItem, DialogLabel, Marquee, Navigation, PanelSection } from '@decky/ui';
import { useCallback, useEffect, useState, type ReactNode } from 'react';
import type { IconType } from 'react-icons';
import {
  FaCheckCircle,
  FaClipboardCheck,
  FaCogs,
  FaExclamationTriangle,
  FaFingerprint,
  FaGamepad,
  FaLinux,
  FaMicrochip,
  FaPuzzlePiece,
  FaRoute,
  FaServer,
  FaShieldAlt,
  FaTimesCircle,
  FaWineBottle
} from 'react-icons/fa';
import { getConfiguration, getStatus } from '../api';
import { readinessError, shouldShowShortcutManagement } from '../shortcut-management/management';
import {
  aggregateReadinessState,
  kvmReadinessState,
  managerReadinessState,
  pathReadinessState,
  ReadinessItem,
  readinessColor
} from '../readiness/readiness-item';
import { getQAMVisualFixture } from '../readiness/visual-fixtures';
import { logger } from '../shared/logger';
import { LoadingSpinner } from '../shared/loading-spinner';
import type { AggregateStatus, Check, Configuration, SystemStatus } from '../types';

export const MANAGEMENT_ROUTE = '/hv-launcher/manage';
export const READINESS_ROUTE = '/hv-launcher/readiness';

const checkIcons: Record<string, IconType> = {
  cpu: FaMicrochip,
  kernel: FaLinux,
  umip: FaShieldAlt,
  'cpuid-fault': FaFingerprint,
  'emulation-module': FaPuzzlePiece,
  proton: FaWineBottle
};

const checkTitles: Record<string, string> = {
  'emulation-module': 'CPUID module',
  proton: 'Proton'
};

const aggregateIcons: Record<AggregateStatus, IconType> = {
  'native-ready': FaCheckCircle,
  'hypervisor-ready': FaCheckCircle,
  'setup-required': FaExclamationTriangle,
  'recovery-required': FaExclamationTriangle,
  unsupported: FaTimesCircle
};

function statusLabel(status: AggregateStatus): string {
  return {
    'native-ready': 'Native CPUID faulting is ready',
    'hypervisor-ready': 'Hypervisor is ready',
    'setup-required': 'Setup is required',
    'recovery-required': 'Manual recovery is required',
    unsupported: 'This system is unsupported'
  }[status];
}

function checkDetail(check: Check, status: SystemStatus): ReactNode {
  if (check.id !== 'cpu') return check.detail;

  return (
    <>
      <Marquee play fadeLength={12}>
        {status.cpu.modelName || status.cpu.vendor}
      </Marquee>
      <div style={{ marginTop: 2 }}>{check.detail}</div>
    </>
  );
}

function humanizeState(state: string): string {
  return state.replaceAll('-', ' ');
}

export function ReadinessContent() {
  const [status, setStatus] = useState<SystemStatus>();
  const [configuration, setConfiguration] = useState<Configuration>();
  const [refreshing, setRefreshing] = useState(false);
  const [error, setError] = useState('');

  const refresh = useCallback(async () => {
    setRefreshing(true);
    try {
      const fixture = getQAMVisualFixture();
      const [nextStatus, nextConfiguration] = fixture
        ? [fixture.status, fixture.configuration]
        : await Promise.all([getStatus(), getConfiguration()]);
      setStatus(nextStatus);
      setConfiguration(nextConfiguration);
      setError('');
    } catch (reason) {
      logger.error('Failed to refresh readiness', reason);
      setError(readinessError(reason));
    } finally {
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  if (!status || !configuration) {
    return (
      <PanelSection title="System readiness">
        <div style={{ padding: '10px 0' }}>{error || <LoadingSpinner />}</div>
        {error && (
          <ButtonItem layout="below" disabled={refreshing} onClick={() => void refresh()}>
            {refreshing ? 'Retrying…' : 'Retry'}
          </ButtonItem>
        )}
      </PanelSection>
    );
  }

  const showManagement = shouldShowShortcutManagement(status, configuration);
  const AggregateIcon = aggregateIcons[status.status];
  const aggregateState = aggregateReadinessState(status.status);
  const pathDetail = status.path === 'native'
    ? 'Native CPUID faulting'
    : status.path === 'hypervisor'
      ? 'Hypervisor CPUID emulation'
      : 'No compatible method';
  const kvmDetail = status.modules.kvmBusy
    ? 'Busy; stop active virtual machines before launching a managed game'
    : status.modules.kvmAmdLoaded || status.modules.kvmLoaded
      ? 'Available'
      : status.modules.controllerState === 'idle'
        ? 'Not loaded; no transition needed'
        : `Not loaded while ${humanizeState(status.modules.controllerState)}`;
  const managerRecovery = status.modules.controllerState === 'recovery-required';

  return (
    <PanelSection title="Readiness check">
      <div style={{ alignItems: 'center', display: 'flex', gap: 9, paddingBottom: 6 }}>
        <AggregateIcon
          aria-hidden
          style={{ color: readinessColor(aggregateState), flexShrink: 0, fontSize: 19 }}
        />
        <DialogLabel style={{ color: readinessColor(aggregateState) }}>
          {statusLabel(status.status)}
        </DialogLabel>
      </div>

      {status.checks.map((check) => (
        <ReadinessItem
          key={check.id}
          icon={checkIcons[check.id] ?? FaClipboardCheck}
          item={{
            title: checkTitles[check.id] ?? check.label,
            detail: checkDetail(check, status),
            state: check.ok ? 'success' : 'error',
            remedy: !check.ok ? check.remedy : undefined
          }}
        />
      ))}

      <ReadinessItem
        icon={FaRoute}
        item={{ title: 'Compatibility method', detail: pathDetail, state: pathReadinessState(status.path) }}
      />
      <ReadinessItem
        icon={FaServer}
        item={{ title: 'KVM', detail: kvmDetail, state: kvmReadinessState(status.modules) }}
      />
      <ReadinessItem
        icon={FaCogs}
        item={{
          title: 'Hypervisor manager',
          detail: humanizeState(status.modules.controllerState),
          state: managerReadinessState(status.modules.controllerState),
          remedy: managerRecovery
            ? 'Module ownership is ambiguous. Restore KVM manually, then restart the plugin before launching managed games.'
            : undefined
        }}
      />

      {status.status === 'native-ready' && !showManagement && (
        <ReadinessItem
          icon={FaGamepad}
          item={{
            title: 'Shortcut management',
            detail: 'No per-shortcut setup is needed on the native path.',
            state: 'info'
          }}
        />
      )}
      {error && (
        <ReadinessItem
          icon={FaExclamationTriangle}
          item={{ title: 'Backend', detail: error, state: 'error' }}
        />
      )}

      {showManagement && (
        <ButtonItem
          layout="below"
          onClick={() => {
            Navigation.Navigate(MANAGEMENT_ROUTE);
            Navigation.CloseSideMenus();
          }}
        >
          Manage shortcuts
        </ButtonItem>
      )}
      <ButtonItem
        layout="below"
        onClick={() => {
          Navigation.Navigate(READINESS_ROUTE);
          Navigation.CloseSideMenus();
        }}
      >
        Readiness details and setup
      </ButtonItem>
      <ButtonItem layout="below" disabled={refreshing} onClick={() => void refresh()}>
        {refreshing ? 'Refreshing…' : 'Refresh status'}
      </ButtonItem>
    </PanelSection>
  );
}
