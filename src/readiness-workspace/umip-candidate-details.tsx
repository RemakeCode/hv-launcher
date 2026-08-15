import { Field } from '@decky/ui';
import type { UMIPBootloader, UMIPCandidate } from '@/types';

interface UMIPCandidateDetailsProps {
  candidate: UMIPCandidate;
}

export function UMIPCandidateDetails({ candidate }: UMIPCandidateDetailsProps) {
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
