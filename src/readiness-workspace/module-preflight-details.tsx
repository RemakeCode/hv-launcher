import { Field } from '@decky/ui';
import { FaCheckCircle, FaPuzzlePiece } from 'react-icons/fa';
import { ReadinessItem } from '@/readiness/readiness-item';
import type { ModulePreflight } from '@/types';

interface ModulePreflightDetailsProps {
  preflight: ModulePreflight;
}

export function ModulePreflightDetails({ preflight }: ModulePreflightDetailsProps) {
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
        <Field
          label='Kernel lockdown (informational)'
          description={`${preflight.lockdown}; acceptance is determined by the guarded load test.`}
        />
      )}
    </>
  );
}
