import type { ReactNode } from 'react';
import type { IconType } from 'react-icons';
import {
  FaCheckCircle,
  FaExclamationTriangle,
  FaInfoCircle,
  FaSyncAlt
} from 'react-icons/fa';
import type { AggregateStatus, SystemStatus } from '../types';

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
    <div
      style={{
        alignItems: 'start',
        borderBottom: '1px solid rgba(255, 255, 255, 0.08)',
        display: 'grid',
        gap: 10,
        gridTemplateColumns: '22px minmax(0, 1fr) 20px',
        padding: '10px 0'
      }}
    >
      <ItemIcon aria-hidden style={{ fontSize: 17, marginTop: 2, opacity: 0.85 }} />
      <div style={{ minWidth: 0 }}>
        <div style={{ fontWeight: 600 }}>{item.title}</div>
        <div style={{ marginTop: 2, opacity: 0.78 }}>{item.detail}</div>
        {item.remedy && (
          <div style={{ color: readinessColor('error'), marginTop: 5 }}>{item.remedy}</div>
        )}
      </div>
      <span aria-label={presentation.label} title={presentation.label}>
        <StatusIcon aria-hidden style={{ color: presentation.color, fontSize: 17, marginTop: 2 }} />
      </span>
    </div>
  );
}
