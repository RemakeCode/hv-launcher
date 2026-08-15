import {
  ConfirmModal,
  DialogButton,
  DialogLabel,
  Field,
  Navigation,
  PanelSection,
  ProgressBarWithInfo,
  showModal,
} from "@decky/ui";
import { FaCheckCircle, FaExclamationTriangle, FaExternalLinkAlt, FaTrash } from "react-icons/fa";
import { ReadinessItem } from "@/readiness/readiness-item";
import type { RuntimeSetupStatus, SetupJobSnapshot } from "@/types";
import { runtimeAction } from '@/readiness-workspace/linuwux/runtime-state';

export interface RuntimeSetupProps {
  setup?: RuntimeSetupStatus;
  job?: SetupJobSnapshot;
  mutationActive: boolean;
  error?: string;
  onAction(action: "install" | "update" | "repair"): void;
  onRemove(): void;
}

const REPOSITORY_URL = "https://github.com/brcly/linuwux-runtime";

const runtimeSetupStyles = `
  .hv-runtime-description {
    margin-bottom: 8px;
  }

  .hv-runtime-action {
    align-items: center;
    display: flex;
    gap: 8px;
  }
`;

function runtimeDetail(runtime: RuntimeSetupStatus["runtime"]): string {
  if (!runtime.available) return runtime.detail ?? humanize(runtime.state);
  const version = runtime.versionKnown && runtime.version ? `v${runtime.version}` : "version unknown";
  if (runtime.updateState === "update-available" && runtime.latestVersion) {
    return `${version} · v${runtime.latestVersion} is available`;
  }
  if (runtime.updateState === "current") return `${version} · current`;
  return `${version} · ${runtime.path}`;
}

function actionLabel(action: "install" | "update" | "repair"): string {
  if (action === "update") return "Runtime update available";
  if (action === "repair") return "Repair runtime installation";
  return "Install the runtime";
}

function actionButton(action: "install" | "update" | "repair"): string {
  if (action === "update") return "Update runtime";
  if (action === "repair") return "Repair runtime";
  return "Install runtime";
}

function humanize(value: string): string {
  return value.replaceAll("-", " ");
}

export function RuntimeSetup({ setup, job, mutationActive, error, onAction, onRemove }: RuntimeSetupProps) {
  const runtime = setup?.runtime;
  const running = job?.kind === "runtime-install" && job.state === "running";
  const action = runtimeAction(setup);
  const releaseTag = setup?.latest?.tag;
  const source = setup?.repository ?? "brcly/linuwux-runtime";
  const latestUpdate = job?.output?.length ? job.output[job.output.length - 1] : undefined;
  const canRemove = runtime && (runtime.available || runtime.state === "invalid");

  const confirmRemove = () => {
    showModal(
      <ConfirmModal
        strTitle="Remove LinUwUx runtime?"
        strDescription="This removes the user-scoped LinUwUx runtime files. Managed shortcuts using runtime must be switched to Proton or disabled first."
        strOKButtonText="Remove runtime"
        strCancelButtonText="Cancel"
        onOK={onRemove}
      />,
    );
  };

  return (
    <PanelSection title="LinUwUx runtime">
      <style>{runtimeSetupStyles}</style>
      <DialogLabel className="hv-runtime-description">
        Uses the LinUwUx runtime with an unpatched GE-Proton or CachyOS Proton build selected in Steam.
      </DialogLabel>

      {runtime && (
        <ReadinessItem
          icon={runtime.available ? FaCheckCircle : FaExclamationTriangle}
          item={{
            title: runtime.available ? "Runtime installed" : runtime.state === "unsupported" ? "Runtime unsupported" : "Runtime setup required",
            detail: runtimeDetail(runtime),
            state: runtime.available ? "success" : "error",
          }}
        />
      )}

      <Field
        label="Release source"
        description={`${source}${releaseTag ? ` · ${releaseTag}` : " · latest stable release"}`}
      >
        <DialogButton onClick={() => Navigation.NavigateToExternalWeb(REPOSITORY_URL)}>
          <span className="hv-runtime-action">
            LinUwUx by brcly <FaExternalLinkAlt aria-hidden />
          </span>
        </DialogButton>
      </Field>
      {canRemove && (
        <Field
          label="Runtime installation"
          description="Remove the user-scoped LinUwUx runtime files."
          inlineWrap="shift-children-below"
        >
          <DialogButton disabled={mutationActive} onClick={confirmRemove}>
            <span className="hv-runtime-action">
              <FaTrash aria-hidden />
              Remove runtime
            </span>
          </DialogButton>
        </Field>
      )}
      {running && latestUpdate && <Field label="Latest update" description={latestUpdate} />}
      {running && job && (
        <ProgressBarWithInfo nProgress={job.progress} sOperationText={humanize(job.phase)} />
      )}
      {setup?.updateError && <Field label="Update status unknown" description={setup.updateError} />}
      {(error || (job?.state === "failed" ? job.error : undefined)) && (
        <ReadinessItem
          icon={FaExclamationTriangle}
          item={{
            title: "Runtime setup did not complete",
            detail: error ?? job?.error ?? "Review the setup log and retry.",
            state: "error",
          }}
        />
      )}

      {action && !running && (
        <Field
          label={actionLabel(action)}
          description="Downloads liblinuwux.so, linuwux.sh, and SHA256SUMS from one exact tagged release and verifies them before installation."
          inlineWrap="shift-children-below"
        >
          <DialogButton disabled={mutationActive} onClick={() => onAction(action)}>
            {actionButton(action)}
          </DialogButton>
        </Field>
      )}
    </PanelSection>
  );
}
