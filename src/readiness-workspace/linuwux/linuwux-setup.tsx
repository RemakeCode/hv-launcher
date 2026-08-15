import type { Dispatch } from "react";
import type {
  RuntimeSetupStatus,
  SetupJobSnapshot,
  SystemStatus,
} from "@/types";
import { ProtonSetup } from '@/readiness-workspace/linuwux/proton-setup';
import type { ProtonDraft, ProtonDraftAction } from '@/readiness-workspace/linuwux/proton-state';
import { RuntimeSetup } from '@/readiness-workspace/linuwux/runtime-setup';

export interface LinUwUxSetupProps {
  status: SystemStatus;
  protonDraft: ProtonDraft;
  runtimeSetup?: RuntimeSetupStatus;
  runtimeJob?: SetupJobSnapshot;
  runtimeError?: string;
  mutationActive: boolean;
  onProtonDraft: Dispatch<ProtonDraftAction>;
  onRuntimeAction(action: "install" | "update" | "repair"): void;
  onRuntimeRemove(): void;
}

export function LinUwUxSetup({
  status,
  protonDraft,
  runtimeSetup,
  runtimeJob,
  runtimeError,
  mutationActive,
  onProtonDraft,
  onRuntimeAction,
  onRuntimeRemove,
}: LinUwUxSetupProps) {
  return (
    <>
      <ProtonSetup
        draft={protonDraft}
        installedTools={status.linuwux.proton.tools}
        mutationActive={mutationActive}
        onDraft={onProtonDraft}
      />
      <RuntimeSetup
        setup={runtimeSetup}
        job={runtimeJob}
        mutationActive={mutationActive}
        error={runtimeError}
        onAction={onRuntimeAction}
        onRemove={onRuntimeRemove}
      />
    </>
  );
}
