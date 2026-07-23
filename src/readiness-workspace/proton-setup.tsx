import { FileSelectionType, openFilePicker } from '@decky/api';
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
import { FaCheckCircle, FaExclamationTriangle, FaWineBottle } from 'react-icons/fa';
import { installProtonArchive, preflightProtonArchive } from '../api';
import { ReadinessItem } from '../readiness/readiness-item';
import { readinessError } from '../shortcut-management/management';
import { setupEventStore } from '../setup-events';
import { LoadingSpinner } from '../shared/loading-spinner';
import { logger } from '../shared/logger';
import {
    isFilePickerCancellation,
    isSupportedProtonArchive,
    type ProtonDraft,
    type ProtonDraftAction
} from './readiness-workspace-state';

const PROTON_PICKER_START_PATH = '/home';
const SETUP_INTERRUPTION_WARNING =
    'Do not update or uninstall HV Launcher, restart Decky Loader, or power off the system until installation finishes.';
// Decky filters by the final suffix; exact multi-suffix validation happens after selection.
const PROTON_PICKER_EXTENSIONS = ['gz', 'tgz', 'xz'];

interface ProtonSetupProps {
    draft: ProtonDraft;
    installedTools: string[];
    mutationActive: boolean;
    onDraft: Dispatch<ProtonDraftAction>;
}

export function ProtonSetup({ draft, installedTools, mutationActive, onDraft }: ProtonSetupProps) {
    const progressVisible = draft.stage === 'installing' || draft.stage === 'completing';
    const selectArchive = async () => {
        const previous = draft;
        try {
            const picked = await openFilePicker(
                FileSelectionType.FILE,
                PROTON_PICKER_START_PATH,
                true,
                true,
                undefined,
                PROTON_PICKER_EXTENSIONS,
                false,
                false
            );
            const path = picked?.realpath || picked?.path;
            if (!path) {
                onDraft({ type: 'selection-cancelled', previous });
                return;
            }
            if (!isSupportedProtonArchive(path)) {
                throw new Error('Select a .tar.gz, .tgz, or .tar.xz Proton archive.');
            }
            onDraft({ type: 'selection-started' });
            const selection = await preflightProtonArchive(path);
            onDraft({ type: 'selection-ready', path, selection });
        } catch (reason) {
            if (isFilePickerCancellation(reason)) {
                onDraft({ type: 'selection-cancelled', previous });
                return;
            }
            logger.error('Failed to check the selected Proton archive', reason);
            onDraft({ type: 'failed', error: readinessError(reason) });
        }
    };

    const startInstall = async () => {
        const selection = draft.selection;
        const destinationId = draft.destinationId;
        if (!selection || !draft.archivePath || !destinationId) return;
        onDraft({ type: 'install-requested' });
        try {
            const job = await installProtonArchive(draft.archivePath, destinationId);
            const latest = setupEventStore.current('proton-install');
            onDraft({ type: 'job-started', job: latest?.id === job.id ? latest : job });
        } catch (reason) {
            logger.error('Failed to start Proton installation', reason);
            onDraft({ type: 'failed', error: readinessError(reason) });
        }
    };

    const confirmInstall = () => {
        const selection = draft.selection;
        if (!selection) return;
        showModal(
            <ConfirmModal
                strTitle='Confirm Proton archive source'
                strDescription={`${selection.responsibility} ${SETUP_INTERRUPTION_WARNING}`}
                strOKButtonText='Install archive'
                strCancelButtonText='Cancel'
                onOK={() => void startInstall()}
            />
        );
    };

    return (
        <PanelSection title='Proton'>
            <Field
                label='Installed supported builds'
                description={
                    installedTools.length > 0
                        ? installedTools.map((tool) => <div key={tool}>{tool}</div>)
                        : 'No supported LinUwUx builds installed.'
                }
            />

            {draft.lastInstall && (
                <ReadinessItem
                    icon={FaCheckCircle}
                    item={{
                        title: `${draft.lastInstall.toolName} installed`,
                        detail: 'Restart Steam, then select the installed compatibility tool for the game.',
                        state: 'success'
                    }}
                />
            )}

            <Field
                label={installedTools.length > 0 ? 'Install another build' : 'Install a build'}
                description='Choose the Proton archive you obtained from the release source. Full validation happens during installation.'
                inlineWrap='shift-children-below'
            >
                {!progressVisible && (
                    <DialogButton
                        disabled={mutationActive || draft.stage === 'selecting'}
                        onClick={() => void selectArchive()}
                    >
                        {draft.selection
                            ? 'Choose another archive'
                            : installedTools.length > 0
                              ? 'Choose another Proton archive'
                              : 'Choose Proton archive'}
                    </DialogButton>
                )}
            </Field>

            {draft.stage === 'selecting' && (
                <>
                    <Field
                        label='Checking selected file'
                        description='Confirming its file type, size, and available Steam destinations…'
                    />
                    <LoadingSpinner />
                </>
            )}
            {draft.selection && <ProtonSelection draft={draft} onDraft={onDraft} />}
            {draft.stage === 'installing' && !draft.job && (
                <Field label='Starting installation' description='Preparing the selected Proton installation…' />
            )}
            {progressVisible && <Field label='Do not interrupt setup' description={SETUP_INTERRUPTION_WARNING} />}
            {progressVisible && draft.job && (
                <>
                    <ProgressBarWithInfo nProgress={draft.job.progress} sOperationText={humanize(draft.job.phase)} />
                </>
            )}
            {draft.error && (
                <ReadinessItem
                    icon={FaExclamationTriangle}
                    item={{ title: 'Installation did not complete', detail: draft.error, state: 'error' }}
                />
            )}
            {draft.selection && !progressVisible && (
                <Field
                    label='Install selected archive'
                    description='The archive will be validated before it is added to the selected Steam installation.'
                    inlineWrap='shift-children-below'
                >
                    <DialogButton disabled={mutationActive || !draft.destinationId} onClick={confirmInstall}>
                        Install Proton
                    </DialogButton>
                </Field>
            )}
        </PanelSection>
    );
}

function ProtonSelection({ draft, onDraft }: Pick<ProtonSetupProps, 'draft' | 'onDraft'>) {
    const selection = draft.selection;
    if (!selection) return null;
    const preflight = selection.preflight;
    return (
        <>
            <ReadinessItem
                icon={FaWineBottle}
                item={{
                    title: preflight.fileName,
                    detail: `${preflight.compression.toUpperCase()} archive · ${formatBytes(preflight.compressedBytes)}`,
                    state: 'success'
                }}
            />
            {preflight.destinations.length > 1 ? (
                <>
                    <Field
                        label='Installation destination'
                        description='Choose which Steam installation should receive this compatibility tool.'
                    />
                    <DropdownItem
                        label='Steam installation'
                        rgOptions={preflight.destinations.map((destination) => ({
                            data: destination.id,
                            label: destination.label
                        }))}
                        selectedOption={draft.destinationId}
                        onChange={(option) =>
                            onDraft({ type: 'destination-selected', destinationId: String(option.data) })
                        }
                    />
                </>
            ) : preflight.destinations.length === 1 ? (
                <Field label='Steam installation' description={preflight.destinations[0].label} />
            ) : (
                <ReadinessItem
                    icon={FaExclamationTriangle}
                    item={{
                        title: 'Steam installation not found',
                        detail: 'Open Steam once, then return and select the archive again.',
                        state: 'error'
                    }}
                />
            )}
        </>
    );
}

function formatBytes(bytes: number): string {
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function humanize(value: string): string {
    return value.replaceAll('-', ' ');
}
