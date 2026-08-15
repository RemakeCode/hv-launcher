import type { ReactNode } from 'react';
import type { IconType } from 'react-icons';
import {
  FaCheckCircle,
  FaExclamationTriangle,
  FaInfoCircle,
  FaSyncAlt
} from 'react-icons/fa';
import type { AggregateStatus, SystemStatus } from '@/types';

export type ReadinessState = 'success' | 'info' | 'active' | 'warning' | 'error';

export interface ReadinessItemData {
  title: string;
  detail: ReactNode;
  state: ReadinessState;
  remedy?: ReactNode;
}

interface ReadinessItemProps {
  icon: IconType;
  item: ReadinessItemData;
}

const statePresentation: Record<ReadinessState, {
  color: string;
  icon: IconType;
  label: string;
}> = {
  success: { color: '#59bf40', icon: FaCheckCircle, label: 'Ready' },
  info: { color: '#66c0f4', icon: FaInfoCircle, label: 'Information' },
  active: { color: '#66c0f4', icon: FaSyncAlt, label: 'In progress' },
  warning: { color: '#e5af37', icon: FaExclamationTriangle, label: 'Attention required' },
  error: { color: '#ff6b6b', icon: FaExclamationTriangle, label: 'Action required' }
};

const readinessItemStyles = `
  .hv-readiness-item {
    align-items: start;
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
    display: grid;
    gap: 10px;
    grid-template-columns: 22px minmax(0, 1fr) 20px;
    padding: 10px 0;
  }

  .hv-readiness-item__icon {
    font-size: 17px;
    margin-top: 2px;
    opacity: 0.85;
  }

  .hv-readiness-item__content {
    min-width: 0;
  }

  .hv-readiness-item__title {
    font-weight: 600;
  }

  .hv-readiness-item__detail {
    margin-top: 2px;
    opacity: 0.78;
  }

  .hv-readiness-item__remedy {
    color: #ff6b6b;
    margin-top: 5px;
  }

  .hv-readiness-item__status-icon {
    font-size: 17px;
    margin-top: 2px;
  }
`;

export function readinessColor(state: ReadinessState): string {
  return statePresentation[state].color;
}

export function aggregateReadinessState(status: AggregateStatus): ReadinessState {
  if (status === 'native-ready' || status === 'hypervisor-ready') return 'success';
  if (status === 'setup-required') return 'warning';
  return 'error';
}

export function pathReadinessState(path: SystemStatus['path']): ReadinessState {
  return path === 'none' ? 'error' : 'success';
}

export function kvmReadinessState(modules: SystemStatus['modules']): ReadinessState {
  if (modules.kvmBusy) return 'error';
  if (modules.kvmAmdLoaded || modules.kvmLoaded) return 'success';
  return modules.controllerState === 'idle' ? 'success' : 'active';
}

export function managerReadinessState(state: string): ReadinessState {
  if (state === 'recovery-required') return 'error';
  if (state === 'idle') return 'success';
  return 'active';
}

export function ReadinessItem({ icon: ItemIcon, item }: ReadinessItemProps) {
  const presentation = statePresentation[item.state];
  const StatusIcon = presentation.icon;

  return (
    <>
      <style>{readinessItemStyles}</style>
      <div className='hv-readiness-item'>
        <ItemIcon aria-hidden className='hv-readiness-item__icon' />
        <div className='hv-readiness-item__content'>
          <div className='hv-readiness-item__title'>{item.title}</div>
          <div className='hv-readiness-item__detail'>{item.detail}</div>
          {item.remedy && <div className='hv-readiness-item__remedy'>{item.remedy}</div>}
        </div>
      <span aria-label={presentation.label} title={presentation.label}>
          <StatusIcon aria-hidden className='hv-readiness-item__status-icon' style={{ color: presentation.color }} />
      </span>
      </div>
    </>
  );
}
