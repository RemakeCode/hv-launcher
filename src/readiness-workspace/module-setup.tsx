import { FileSelectionType, openFilePicker } from '@decky/api';
import {
  ConfirmModal,
  DialogButton,
  Field,
  PanelSection,
  ProgressBarWithInfo,
  showModal
} from '@decky/ui';
import { useState, type Dispatch } from 'react';
import { FaCheckCircle, FaExclamationTriangle, FaPuzzlePiece } from 'react-icons/fa';
import { installModuleArchive, testModule } from '@/api';
import { ReadinessItem } from '@/readiness/readiness-item';
import { issueSetupCapability, MODULE_TEST_CAPABILITY_BINDING } from '@/setup-capability';
import { setupEventStore } from '@/setup-events';
import { LoadingSpinner } from '@/shared/loading-spinner';
import { logger } from '@/shared/logger';
import { readinessError } from '@/shortcut-management/management';
import type { Check, ModulePreflight, SystemStatus } from '@/types';
import { isFilePickerCancellation } from '@/readiness-workspace/readiness-workspace-state';
import type { ModuleDraft, ModuleDraftAction } from '@/readiness-workspace/readiness-workspace-state';

const MODULE_PICKER_START_PATH = '/home';
const SETUP_INTERRUPTION_WARNING =
  'Do not update or uninstall HV Launcher, restart Decky Loader, or power off the system until installation finishes. Interrupting DKMS setup may require manual cleanup before trying again.';

interface ModuleSetupProps {
  check: Check;
  draft: ModuleDraft;
  preflight?: ModulePreflight;
  mutationActive: boolean;
  status: SystemStatus;
  onRefresh: () => Promise<void>;
  onDraft: Dispatch<ModuleDraftAction>;
}

export function ModuleSetup({ check, draft, preflight, mutationActive, status, onRefresh, onDraft }: ModuleSetupProps) {
  const [testing, setTesting] = useState(false);
  const [testError, setTestError] = useState('');
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

  const runModuleTest = async () => {
    setTesting(true);
    setTestError('');
    try {
      const capability = await issueSetupCapability('module-test', MODULE_TEST_CAPABILITY_BINDING);
      const result = await testModule(capability);
      if (result.error) setTestError(result.error);
      await onRefresh();
    } catch (reason) {
      logger.error('Failed to test the CPUID module', reason);
      setTestError(readinessError(reason));
    } finally {
      setTesting(false);
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
        strDescription={`HV Launcher cannot verify this archive's origin. DKMS will execute its Makefile as root. ${dependencyDescription} Continue only if you sourced the intended archive. ${SETUP_INTERRUPTION_WARNING}`}
        strOKButtonText='Install module'
        strCancelButtonText='Cancel'
        onOK={() => void startInstall()}
      />
    );
  };

  const installAllowed = Boolean(draft.archivePath) &&
    (!preflight || preflight.controllerState === 'idle') &&
    (preflight?.ready || Boolean(dependencyPlan));
  const verificationState = status.modules.verificationState ?? 'pending';
  const testBlocked = status.modules.controllerState === 'recovery-required' ||
    status.modules.controllerState !== 'idle' ||
    status.modules.kvmBusy ||
    !status.modules.emulationInstalled ||
    !status.modules.emulationCompatible;
  const testUnavailableReason = status.modules.controllerState === 'recovery-required'
    ? 'Testing is disabled until controller recovery is completed.'
    : status.modules.controllerState !== 'idle'
      ? 'The hypervisor manager is busy; retry when it returns to idle.'
      : status.modules.kvmBusy
        ? 'KVM is busy; stop active virtual machines before testing the module.'
        : !status.modules.emulationInstalled
          ? 'Install the CPUID module for the running kernel before testing it.'
          : !status.modules.emulationCompatible
            ? 'The installed CPUID module does not match the running kernel.'
            : '';

  return (
    <PanelSection title='CPUID module'>
      {preflight ? <ModulePreflightDetails preflight={preflight} /> : <LoadingSpinner />}

      {draft.result && (
        <ReadinessItem
          icon={draft.result.signaturePresent === false ? FaExclamationTriangle : FaCheckCircle}
          item={{
            title: draft.result.noOp ? 'Module already installed' : 'Module installed',
            detail: draft.result.signaturePresent === false
              ? 'No generated-module signature metadata was reported'
              : `Installed for kernel ${draft.result.kernelRelease}`,
            state: 'info'
          }}
        />
      )}

      {status.modules.controllerState === 'recovery-required' && (
        <ReadinessItem
          icon={FaExclamationTriangle}
          item={{
            title: 'Recovery required',
            detail: 'Restore KVM manually, then restart the plugin.',
            state: 'error'
          }}
        />
      )}

      <ReadinessItem
        icon={verificationState === 'verified' ? FaCheckCircle : FaExclamationTriangle}
        item={{
          title: 'Module verification',
          detail: verificationState === 'verified'
            ? 'The running kernel accepted cpuid_fault_emulation'
            : verificationState === 'failed'
              ? (status.modules.verificationDetail ?? 'The running kernel did not accept cpuid_fault_emulation.')
              : 'Unable to test if module will load on this kernel, you can test manually or run a game',
          state: verificationState === 'verified' ? 'success' : verificationState === 'failed' ? 'error' : 'warning',
          remedy: status.modules.verificationRemediation
        }}
      />
      <Field
        label='Installation and signing state'
        description={!status.modules.emulationInstalled
          ? 'Not installed for the running kernel.'
          : !status.modules.emulationCompatible
            ? 'Installed module does not match the running kernel.'
            : status.modules.signaturePresent
              ? `Signature metadata present${status.modules.signer ? ` (${status.modules.signer})` : ''}; kernel trust is determined by the guarded load test.`
              : 'No generated kernel-module signature metadata was reported'}
      />
      {status.modules.verificationDetail && verificationState !== 'failed' && (
        <Field label='Last module-test detail' description={status.modules.verificationDetail} />
      )}
      {testError && <ReadinessItem icon={FaExclamationTriangle} item={{ title: 'Module test', detail: testError, state: 'error' }} />}
      <Field
        label='Test module'
        description={testBlocked ? testUnavailableReason : 'Run the guarded post-install test; the host is restored afterward.'}
        inlineWrap='shift-children-below'
      >
        <DialogButton disabled={mutationActive || testing || testBlocked} onClick={() => void runModuleTest()}>
          {testing ? 'Testing module…' : 'Test module'}
        </DialogButton>
      </Field>

      {!preflight?.ready && preflight?.dependencyPlanError && (
        <ReadinessItem
          icon={FaExclamationTriangle}
          item={{ title: 'Dependencies need manual setup', detail: preflight.dependencyPlanError, state: 'error', remedy: check.remedy }}
        />
      )}

      <Field
        label={draft.archivePath ? 'Choose another source' : 'Install the module'}
        description='Choose the CPUID module ZIP archive from the release source.'
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
          <Field label='Selecting source archive' description='Choose the ZIP archive from the release source.' />
          <LoadingSpinner />
        </>
      )}
      {draft.archivePath && !progressVisible && draft.stage !== 'selecting' && (
        <Field label='Selected source archive' description={draft.archivePath} />
      )}
      {progressVisible && <Field label='Do not interrupt setup' description={SETUP_INTERRUPTION_WARNING} />}
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
        <Field label='Install reviewed module' description='Validate, prepare dependencies, and build for the running kernel.' inlineWrap='shift-children-below'>
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
        <Field label='Kernel lockdown (informational)' description={`${preflight.lockdown}; acceptance is determined by the guarded load test.`} />
      )}
    </>
  );
}

function humanize(value: string): string {
  return value.replaceAll('-', ' ');
}
