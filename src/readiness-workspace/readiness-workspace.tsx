import { Field, PanelSection, SidebarNavigation } from '@decky/ui';
import { useCallback, useEffect, useReducer, useState } from 'react';
import { FaCheckCircle, FaExclamationTriangle, FaPuzzlePiece, FaShieldAlt, FaWineBottle } from 'react-icons/fa';
import type { IconType } from 'react-icons';
import { getStatus, getUMIPInspection } from '../api';
import { READINESS_ROUTE } from '../qam/qam';
import { ReadinessItem, readinessColor } from '../readiness/readiness-item';
import {
  getQAMVisualFixture,
  getReadinessWorkspaceProtonFixture,
  getReadinessWorkspaceUMIPFixture
} from '../readiness/visual-fixtures';
import { LoadingSpinner } from '../shared/loading-spinner';
import { logger } from '../shared/logger';
import { readinessError } from '../shortcut-management/management';
import { setupEventStore } from '../setup-events';
import type { Check, SystemStatus } from '../types';
import { ProtonSetup } from './proton-setup';
import { UMIPSetup } from './umip-setup';
import {
  emptyUMIPDraft,
  emptyProtonDraft,
  initialReadinessSelection,
  protonDraftReducer,
  readinessPageLink,
  readinessSelectionFromPage,
  readinessWorkspaceChecks,
  umipDraftReducer
} from './readiness-workspace-state';

const workspaceIcons: Record<string, IconType> = {
  umip: FaShieldAlt,
  'emulation-module': FaPuzzlePiece,
  proton: FaWineBottle
};

const workspaceTitles: Record<string, string> = {
  'emulation-module': 'CPUID module',
  proton: 'Proton'
};

const PROTON_COMPLETION_HOLD_MS = 2_000;

export function ReadinessWorkspace() {
  const [status, setStatus] = useState<SystemStatus>();
  const [selected, setSelected] = useState('');
  const [error, setError] = useState('');
  const [protonDraft, dispatchProton] = useReducer(
    protonDraftReducer,
    getReadinessWorkspaceProtonFixture() ?? emptyProtonDraft
  );
  const [umipDraft, dispatchUMIP] = useReducer(
    umipDraftReducer,
    getReadinessWorkspaceUMIPFixture() ?? emptyUMIPDraft
  );
  const [mutationActive, setMutationActive] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const fixture = getQAMVisualFixture();
      const next = fixture?.status ?? (await getStatus());
      const checks = readinessWorkspaceChecks(next.checks);
      setStatus(next);
      setSelected((current) => current || initialReadinessSelection(checks));
      setError('');
      if (checks.some((check) => check.id === 'umip')) {
        const visualUMIP = getReadinessWorkspaceUMIPFixture();
        if (fixture && visualUMIP?.inspection) {
          dispatchUMIP({ type: 'inspection-loaded', inspection: visualUMIP.inspection });
          return;
        }
        try {
          dispatchUMIP({ type: 'inspection-loaded', inspection: await getUMIPInspection() });
        } catch (reason) {
          logger.error('Failed to inspect UMIP boot configuration', reason);
          dispatchUMIP({ type: 'inspection-failed', error: readinessError(reason) });
        }
      }
    } catch (reason) {
      logger.error('Failed to load the readiness workspace', reason);
      setError(readinessError(reason));
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(
    () =>
      setupEventStore.subscribe((job) => {
        setMutationActive(job.state === 'running');
        dispatchProton({ type: 'job-updated', job });
        dispatchUMIP({ type: 'job-updated', job });
        if ((job.kind === 'proton-install' || job.kind === 'umip-apply') && job.state !== 'running') {
          void refresh();
        }
      }),
    [refresh]
  );

  useEffect(() => {
    if (protonDraft.stage !== 'completing') return;
    const timer = window.setTimeout(
      () => {
        setupEventStore.dismiss('proton-install');
        dispatchProton({ type: 'completion-displayed' });
      },
      PROTON_COMPLETION_HOLD_MS
    );
    return () => window.clearTimeout(timer);
  }, [protonDraft.stage]);

  if (!status) {
    return (
      <PanelSection title='System readiness'>
        <div style={{ padding: 16 }}>{error || <LoadingSpinner />}</div>
      </PanelSection>
    );
  }

  const checks = readinessWorkspaceChecks(status.checks);
  const pages = checks.map((check) => {
    const Icon = workspaceIcons[check.id] ?? FaExclamationTriangle;
    const content = check.id === 'proton' ? (
      <ProtonSetup
        draft={protonDraft}
        installedTools={status.proton.tools}
        mutationActive={mutationActive}
        onDraft={dispatchProton}
      />
    ) : check.id === 'umip' ? (
      <UMIPSetup
        check={check}
        draft={umipDraft}
        mutationActive={mutationActive}
        onDraft={dispatchUMIP}
      />
    ) : (
      <ReadinessDetail check={check} />
    );

    return {
      route: READINESS_ROUTE,
      link: readinessPageLink(READINESS_ROUTE, check.id),
      title: (
        <div style={{ alignItems: 'center', display: 'flex', gap: 8 }}>
          {check.ok ? (
            <FaCheckCircle style={{ color: readinessColor('success') }} />
          ) : (
            <FaExclamationTriangle style={{ color: readinessColor('error') }} />
          )}
          <span>{workspaceTitles[check.id] ?? check.label}</span>
        </div>
      ),
      icon: <Icon />,
      content
    };
  });

  return (
    <SidebarNavigation
      title='System readiness'
      pages={pages}
      page={readinessPageLink(READINESS_ROUTE, selected)}
      onPageRequested={(page) => {
        const requested = readinessSelectionFromPage(page, READINESS_ROUTE, checks);
        if (requested) setSelected(requested);
      }}
      showTitle
    />
  );
}

interface ReadinessDetailProps {
  check: Check;
}

function ReadinessDetail({ check }: ReadinessDetailProps) {
  return (
    <PanelSection title={workspaceTitles[check.id] ?? check.label}>
      <ReadinessItem
        icon={check.ok ? FaCheckCircle : FaExclamationTriangle}
        item={{
          title: check.ok ? 'Ready' : 'Action needed',
          detail: check.detail,
          state: check.ok ? 'success' : 'error',
          remedy: check.remedy
        }}
      />
      {!check.ok && (
        <Field
          label='Manual setup'
          description={check.remedy ?? 'Follow the release instructions, then reopen this page to check again.'}
        />
      )}
    </PanelSection>
  );
}
