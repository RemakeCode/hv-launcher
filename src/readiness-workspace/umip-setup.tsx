import {
  ConfirmModal,
  DialogButton,
  DropdownItem,
  Field,
  PanelSection,
  ProgressBarWithInfo,
  showModal
} from '@decky/ui';
import type { Dispatch } from 'react';
import { FaCheckCircle, FaExclamationTriangle, FaShieldAlt } from 'react-icons/fa';
import { applyUMIPConfiguration } from '@/api';
import { ReadinessItem } from '@/readiness/readiness-item';
import { issueSetupCapability } from '@/setup-capability';
import { setupEventStore } from '../setup-events';
import { LoadingSpinner } from '../shared/loading-spinner';
import { logger } from '../shared/logger';
import { readinessError } from '../shortcut-management/management';
import type { Check, UMIPBootloader, UMIPCandidate } from '../types';
import type { UMIPDraft, UMIPDraftAction } from './readiness-workspace-state';

interface UMIPSetupProps {
  check: Check;
  draft: UMIPDraft;
  mutationActive: boolean;
  onDraft: Dispatch<UMIPDraftAction>;
}

export function UMIPSetup({ check, draft, mutationActive, onDraft }: UMIPSetupProps) {
  const inspection = draft.inspection;
  const candidate = inspection?.candidates.find((item) => item.bootloader === draft.selected);
  const progressVisible = draft.stage === 'applying' && draft.job;

  const startApply = async () => {
    if (!draft.selected) return;
    onDraft({ type: 'apply-requested' });
    try {
      const capability = await issueSetupCapability('umip-apply', `umip-apply:${draft.selected}`);
      const job = await applyUMIPConfiguration(draft.selected, capability);
      const latest = setupEventStore.current('umip-apply');
      onDraft({ type: 'job-started', job: latest?.id === job.id ? latest : job });
    } catch (reason) {
      logger.error('Failed to start UMIP configuration', reason);
      onDraft({ type: 'failed', error: readinessError(reason) });
    }
  };

  const confirmApply = () => {
    if (!candidate) return;
    showModal(
      <ConfirmModal
        strTitle='Update boot configuration?'
        strDescription={`HV Launcher will add clearcpuid=514 to ${candidate.configuration}, run ${formatUpdater(candidate.updater.path, candidate.updater.args)}, and then require a system restart.`}
        strOKButtonText='Update configuration'
        strCancelButtonText='Cancel'
        onOK={() => void startApply()}
      />
    );
  };

  if (!inspection && draft.error) {
    return (
      <PanelSection title='UMIP'>
        <ReadinessItem
          icon={FaExclamationTriangle}
          item={{ title: 'Boot configuration could not be inspected', detail: draft.error, state: 'error' }}
        />
        <Field label='Manual setup' description={check.remedy ?? 'Configure UMIP manually, then restart the system.'} />
      </PanelSection>
    );
  }

  if (!inspection || draft.stage === 'loading') {
    return (
      <PanelSection title='UMIP'>
        <LoadingSpinner />
      </PanelSection>
    );
  }

  if (check.ok) {
    return (
      <PanelSection title='UMIP'>
        <ReadinessItem
          icon={FaCheckCircle}
          item={{ title: 'Ready', detail: check.detail, state: 'success' }}
        />
        {candidate?.existingArgument && (
          <Field label='Boot configuration' description={`${candidate.existingArgument} is already configured.`} />
        )}
      </PanelSection>
    );
  }

  if (inspection.selection === 'manual-only' || inspection.candidates.length === 0) {
    return (
      <PanelSection title='UMIP'>
        <ReadinessItem
          icon={FaExclamationTriangle}
          item={{ title: 'Manual setup required', detail: check.detail, state: 'error', remedy: check.remedy }}
        />
        {inspection.manual.map((outcome, index) => (
          <Field
            key={`${outcome.bootloader ?? 'system'}-${outcome.reason}-${index}`}
            label={outcome.bootloader ? bootloaderLabel(outcome.bootloader) : 'Bootloader'}
            description={outcome.detail}
          />
        ))}
      </PanelSection>
    );
  }

  return (
    <PanelSection title='UMIP'>
      <ReadinessItem
        icon={draft.stage === 'restart-required' ? FaCheckCircle : FaShieldAlt}
        item={{
          title: draft.stage === 'restart-required' ? 'Restart required' : 'Boot configuration required',
          detail: draft.stage === 'restart-required'
            ? 'The UMIP argument is configured. Restart the system; readiness will remain blocked until UMIP is absent from the running CPU flags.'
            : check.detail,
          state: draft.stage === 'restart-required' ? 'info' : 'error',
          remedy: draft.stage === 'restart-required' ? undefined : check.remedy
        }}
      />

      {inspection.selection === 'choice-required' && (
        <DropdownItem
          label='Bootloader'
          description='Choose the boot configuration used by this system.'
          rgOptions={inspection.candidates.map((item) => ({
            data: item.bootloader,
            label: bootloaderLabel(item.bootloader)
          }))}
          selectedOption={draft.selected}
          onChange={(option) => onDraft({
            type: 'bootloader-selected',
            bootloader: String(option.data) as UMIPBootloader
          })}
        />
      )}

      {candidate && <CandidateDetails candidate={candidate} />}

      {candidate?.state === 'action-required' && (
        <>
          <Field label='Current kernel command line' description={candidate.currentValue || '(empty)'} />
          <Field label='Proposed kernel command line' description={candidate.proposedValue} />
          <Field
            label='Updater command'
            description={formatUpdater(candidate.updater.path, candidate.updater.args)}
          />
        </>
      )}

      {progressVisible && (
        <>
          <ProgressBarWithInfo
            nProgress={draft.job?.progress ?? 0}
            sOperationText={humanize(draft.job?.phase ?? 'starting')}
          />
          {draft.job?.output.length ? (
            <Field label='Latest update' description={draft.job.output[draft.job.output.length - 1]} />
          ) : null}
        </>
      )}

      {draft.error && (
        <ReadinessItem
          icon={FaExclamationTriangle}
          item={{ title: 'UMIP setup did not complete', detail: draft.error, state: 'error' }}
        />
      )}

      {candidate?.state === 'action-required' && draft.stage === 'idle' && (
        <Field
          label='Apply change'
          description='Add the fixed UMIP argument and update the selected bootloader.'
          inlineWrap='shift-children-below'
        >
          <DialogButton disabled={mutationActive} onClick={confirmApply}>
            Update boot configuration
          </DialogButton>
        </Field>
      )}

      {draft.stage === 'failure' && candidate?.state === 'action-required' && (
        <Field
          label='Try again'
          description='Read the current configuration and retry the fixed update.'
          inlineWrap='shift-children-below'
        >
          <DialogButton disabled={mutationActive} onClick={confirmApply}>
            Retry update
          </DialogButton>
        </Field>
      )}
    </PanelSection>
  );
}

function CandidateDetails({ candidate }: { candidate: UMIPCandidate }) {
  return (
    <>
      <Field label='Bootloader' description={bootloaderLabel(candidate.bootloader)} />
      <Field label='Configuration file' description={candidate.configuration} />
      {candidate.state === 'restart-required' && (
        <Field label='Configured argument' description={candidate.existingArgument} />
      )}
    </>
  );
}

function bootloaderLabel(bootloader: UMIPBootloader): string {
  return bootloader === 'grub' ? 'GRUB' : 'Limine';
}

function formatUpdater(path: string, args: string[]): string {
  return [path, ...args].join(' ');
}

function humanize(value: string): string {
  return value.replaceAll('-', ' ');
}
