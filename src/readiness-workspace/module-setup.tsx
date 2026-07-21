import { FileSelectionType, openFilePicker } from '@decky/api';
import {
  ConfirmModal,
  DialogButton,
  Field,
  PanelSection,
  ProgressBarWithInfo,
  showModal
} from '@decky/ui';
import type { Dispatch } from 'react';
import { FaCheckCircle, FaExclamationTriangle, FaPuzzlePiece } from 'react-icons/fa';
import { installModuleArchive } from '@/api';
import { ReadinessItem } from '@/readiness/readiness-item';
import { issueSetupCapability } from '@/setup-capability';
import { setupEventStore } from '@/setup-events';
import { LoadingSpinner } from '@/shared/loading-spinner';
import { logger } from '@/shared/logger';
import { readinessError } from '@/shortcut-management/management';
import type { Check, ModulePreflight } from '@/types';
import { isFilePickerCancellation } from '@/readiness-workspace/readiness-workspace-state';
import type { ModuleDraft, ModuleDraftAction } from '@/readiness-workspace/readiness-workspace-state';

const MODULE_PICKER_START_PATH = '/home';

interface ModuleSetupProps {
  check: Check;
  draft: ModuleDraft;
  preflight?: ModulePreflight;
  mutationActive: boolean;
  onDraft: Dispatch<ModuleDraftAction>;
}

export function ModuleSetup({ check, draft, preflight, mutationActive, onDraft }: ModuleSetupProps) {
  const dependencyPlan = preflight?.dependencyPlan;
  const progressVisible = draft.stage === 'installing';

  const selectArchive = async () => {
    onDraft({ type: 'selection-started' });
    try {
      const picked = await openFilePicker(
        FileSelectionType.FILE,
        MODULE_PICKER_START_PATH,
        true,
        true,
        undefined,
        ['zip'],
        false,
        false
      );
      const path = picked?.realpath || picked?.path;
      if (!path) {
        onDraft({ type: 'selection-cancelled' });
        return;
      }
      if (!path.toLowerCase().endsWith('.zip')) {
        throw new Error('Select the cpuid_fault_emulation ZIP archive.');
      }
      onDraft({ type: 'selection-ready', path });
    } catch (reason) {
      if (isFilePickerCancellation(reason)) {
        onDraft({ type: 'selection-cancelled' });
        return;
      }
      logger.error('Failed to select the CPUID module archive', reason);
      onDraft({ type: 'failed', error: readinessError(reason) });
    }
  };

  const startInstall = async () => {
    if (!draft.archivePath) return;
    onDraft({ type: 'install-requested' });
    try {
      const capability = await issueSetupCapability('module-install', draft.archivePath);
      const job = await installModuleArchive(draft.archivePath, capability);
      const latest = setupEventStore.current('module-install');
      onDraft({ type: 'job-started', job: latest?.id === job.id ? latest : job });
    } catch (reason) {
      logger.error('Failed to start CPUID module installation', reason);
      onDraft({ type: 'failed', error: readinessError(reason) });
    }
  };

  const confirmInstall = () => {
    if (!draft.archivePath || (preflight && preflight.controllerState !== 'idle')) return;
    const dependencyDescription = dependencyPlan
      ? `Packages: ${dependencyPlan.packages.join(', ')} using ${dependencyPlan.manager}.${dependencyPlan.previewOutput ? ` Preview: ${dependencyPlan.previewOutput}` : ''}`
      : 'No dependency package transaction is currently required.';
    showModal(
      <ConfirmModal
        strTitle='Confirm CPUID module source'
        strDescription={`HV Launcher cannot verify this archive's origin. DKMS will execute its Makefile as root. ${dependencyDescription} Continue only if you sourced the intended archive.`}
        strOKButtonText='Install module'
        strCancelButtonText='Cancel'
        onOK={() => void startInstall()}
      />
    );
  };

  const installAllowed = Boolean(draft.archivePath) &&
    (!preflight || preflight.controllerState === 'idle') &&
    (preflight?.ready || Boolean(dependencyPlan));

  return (
    <PanelSection title='CPUID module'>
      {preflight ? <ModulePreflightDetails preflight={preflight} /> : <LoadingSpinner />}

      {draft.result && (
        <ReadinessItem
          icon={draft.result.signingRequired ? FaExclamationTriangle : FaCheckCircle}
          item={{
            title: draft.result.noOp ? 'Module already installed' : 'Module installed',
            detail: draft.result.signingRequired
              ? 'The module matches this kernel, but Secure Boot trust still requires manual signing or MOK enrollment.'
              : `Verified for kernel ${draft.result.kernelRelease}.`,
            state: draft.result.signingRequired ? 'error' : 'success'
          }}
        />
      )}

      {!preflight?.ready && preflight?.dependencyPlanError && (
        <ReadinessItem
          icon={FaExclamationTriangle}
          item={{ title: 'Dependencies need manual setup', detail: preflight.dependencyPlanError, state: 'error', remedy: check.remedy }}
        />
      )}

      <Field
        label={draft.archivePath ? 'Choose another source' : 'Install the module'}
        description='Choose the CPUID module ZIP archive you obtained from the release source. Full validation happens during installation.'
        inlineWrap='shift-children-below'
      >
        {!progressVisible && (
          <DialogButton
            disabled={mutationActive || draft.stage === 'selecting'}
            onClick={() => void selectArchive()}
          >
            {draft.archivePath ? 'Choose another module archive' : 'Choose module archive'}
          </DialogButton>
        )}
      </Field>

      {draft.stage === 'selecting' && (
        <>
          <Field label='Selecting source archive' description='Choose the ZIP archive you obtained from the release source.' />
          <LoadingSpinner />
        </>
      )}
      {draft.archivePath && !progressVisible && draft.stage !== 'selecting' && (
        <Field label='Selected source archive' description={draft.archivePath} />
      )}
      {progressVisible && draft.job && (
        <>
          <ProgressBarWithInfo nProgress={draft.job.progress} sOperationText={humanize(draft.job.phase)} />
          {draft.job.output.length > 0 && <Field label='Latest update' description={draft.job.output[draft.job.output.length - 1]} />}
        </>
      )}
      {draft.stage === 'complete' && draft.result?.inspection && (
        <>
          <Field
            label='Validated archive'
            description={`${draft.result.inspection.identity.packageName} ${draft.result.inspection.identity.packageVersion}; ${draft.result.inspection.entryCount} entries checked.`}
          />
          <Field
            label='Structural checks'
            description={`Required files: ${draft.result.inspection.requiredFiles.join(', ')}.`}
          />
        </>
      )}
      {draft.error && (
        <ReadinessItem
          icon={FaExclamationTriangle}
          item={{ title: 'Module setup did not complete', detail: draft.error, state: 'error' }}
        />
      )}

      {draft.archivePath && !progressVisible && draft.stage !== 'complete' && (
        <Field label='Install reviewed module' description='Validate the archive, prepare dependencies, and build for the running kernel.' inlineWrap='shift-children-below'>
          <DialogButton disabled={mutationActive || !installAllowed} onClick={confirmInstall}>Install CPUID module</DialogButton>
        </Field>
      )}
    </PanelSection>
  );
}

function ModulePreflightDetails({ preflight }: { preflight: ModulePreflight }) {
  return (
    <>
      <ReadinessItem
        icon={preflight.ready ? FaCheckCircle : FaPuzzlePiece}
        item={{
          title: preflight.ready ? 'Host requirements ready' : 'Host requirements need attention',
          detail: `${preflight.distributionId ?? 'Unknown distribution'} · kernel ${preflight.kernelRelease || 'unknown'}`,
          state: preflight.ready ? 'success' : 'info'
        }}
      />
      {preflight.dependencyPlan && (
        <>
          <Field
            label='Reviewed dependency transaction'
            description={`${preflight.dependencyPlan.manager}: ${preflight.dependencyPlan.packages.join(', ')}`}
          />
          {preflight.dependencyPlan.previewOutput && (
            <Field label='Package manager preview' description={preflight.dependencyPlan.previewOutput} />
          )}
        </>
      )}
      {preflight.lockdown !== 'none' && preflight.lockdown !== 'unknown' && (
        <Field label='Kernel lockdown' description={`${preflight.lockdown}; module signing may require manual MOK enrollment.`} />
      )}
    </>
  );
}

function humanize(value: string): string {
  return value.replaceAll('-', ' ');
}
