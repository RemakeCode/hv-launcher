import type { Configuration, DisplayState, Game, SystemStatus } from "./types";

export interface ShortcutSections {
  managed: Game[];
  available: Game[];
}

export function shouldShowShortcutManagement(
  status: Pick<SystemStatus, "path">,
  configuration: Configuration,
): boolean {
  return status.path === "hypervisor" ||
    Object.values(configuration.games).some((game) => game.shortcut);
}

export function groupShortcuts(games: Game[]): ShortcutSections {
  const shortcuts = games
    .filter((game) => game.shortcut)
    .sort((left, right) => left.name.localeCompare(right.name));
  return {
    managed: shortcuts.filter((game) => game.enabled),
    available: shortcuts.filter((game) => !game.enabled),
  };
}

export function canChangeShortcut(
  status: SystemStatus["status"],
  game: Game,
): boolean {
  return game.enabled || status === "hypervisor-ready";
}

export function shortcutDescription(
  game: Game,
  state: DisplayState,
  updating: boolean,
): string | undefined {
  const details: string[] = [];
  if (state !== "idle") details.push(state[0].toUpperCase() + state.slice(1));
  if (updating) details.push("Updating…");
  if (game.missing) details.push("Missing from Steam");
  if (game.conflict) details.push(game.conflict);
  return details.length > 0 ? details.join(" · ") : undefined;
}

function errorMessage(reason: unknown): string {
  return reason instanceof Error ? reason.message : String(reason);
}

export function readinessError(reason: unknown): string {
  return errorMessage(reason);
}

export function shortcutActionError(
  game: Game,
  enabled: boolean,
  reason: unknown,
): string {
  return `Failed to ${enabled ? "enable" : "restore"} “${game.name}”: ${errorMessage(reason)}`;
}
