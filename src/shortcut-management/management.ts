import type { Configuration, DisplayState, Game, LinUwUxMode, SystemStatus } from '@/types';

export interface ShortcutSections {
  managed: Game[];
  available: Game[];
}

export function shouldShowShortcutManagement(
  status: Pick<SystemStatus, 'path'> & { linuwux?: Pick<NonNullable<SystemStatus['linuwux']>, 'runtime'> },
  configuration: Configuration
): boolean {
  return (
    status.path === 'hypervisor' ||
    status.linuwux?.runtime?.available === true ||
    Object.values(configuration.games).some((game) => game.shortcut)
  );
}

export function effectiveGameMode(configuration: Configuration, appId: string): LinUwUxMode {
  return configuration.games[appId]?.mode ?? 'proton';
}

export function availableModes(status: SystemStatus): LinUwUxMode[] {
  const modes: LinUwUxMode[] = [];
  if (isModeAvailable(status, 'proton')) modes.push('proton');
  if (isModeAvailable(status, 'runtime')) modes.push('runtime');
  return modes;
}

export function isModeAvailable(status: SystemStatus, mode: LinUwUxMode): boolean {
  return mode === 'proton'
    ? (status.linuwux?.proton ?? status.proton).found
    : status.linuwux?.runtime?.available === true;
}

export function groupShortcuts(games: Game[]): ShortcutSections {
  const shortcuts = games.filter((game) => game.shortcut).sort((left, right) => left.name.localeCompare(right.name));
  return {
    managed: shortcuts.filter((game) => game.enabled),
    available: shortcuts.filter((game) => !game.enabled)
  };
}

export function shortcutDescription(game: Game, state: DisplayState, updating: boolean): string | undefined {
  const details: string[] = [];
  if (state !== 'idle') details.push(state[0].toUpperCase() + state.slice(1));
  if (updating) details.push('Updating…');
  if (game.missing) details.push('Missing from Steam');
  return details.length > 0 ? details.join(' · ') : undefined;
}

function errorMessage(reason: unknown): string {
  return reason instanceof Error ? reason.message : String(reason);
}

export function readinessError(reason: unknown): string {
  return errorMessage(reason);
}

export function shortcutActionError(game: Game, enabled: boolean, reason: unknown): string {
  return `Failed to ${enabled ? 'enable' : 'disable'} “${game.name}”: ${errorMessage(reason)}`;
}
