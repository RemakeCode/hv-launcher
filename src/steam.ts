import type { Configuration, Game } from "./types";
import { disableGame, enableGame, getConfiguration, postLifetime } from "./api";

interface Unregisterable {
  unregister(): void;
}

interface LifetimeNotification {
  unAppID: number;
  nInstanceID: number;
  bRunning: boolean;
}

export interface SteamBridge {
  Apps: {
    SetAppLaunchOptions(appId: number, value: string): void;
    SetShortcutLaunchOptions(appId: number, value: string): void;
    RegisterForAppDetails?: (appId: number, callback: (details: SteamDetails) => void) => void | Unregisterable;
    RegisterForAppOverviewChanges?: (callback: (data: ArrayBuffer) => void) => void | Unregisterable;
  };
  GameSessions: {
    RegisterForAppLifetimeNotifications(callback: (notification: LifetimeNotification) => void): Unregisterable;
  };
}

interface SteamDetails {
  strLaunchOptions?: string;
  strShortcutLaunchOptions?: string;
}

interface SteamDetailsStore {
  GetAppDetails(appId: number): SteamDetails | null;
}

interface SteamPerClientData {
  display_status?: number;
  installed?: boolean;
}

export interface MaterializedAppOverview {
  appid: number;
  display_name: string;
  app_type: number;
  visible_in_game_list: boolean;
  per_client_data?: SteamPerClientData[];
  local_per_client_data?: SteamPerClientData;
  most_available_per_client_data?: SteamPerClientData;
}

export interface MaterializedAppStore {
  m_bIsInitialized: boolean;
  allApps?: MaterializedAppOverview[];
  GetAppOverviewByAppID(appId: number): MaterializedAppOverview | null;
}

const APP_TYPE_GAME = 1;
const APP_TYPE_SHORTCUT = 0x40000000;

export class SteamLibraryLoadingError extends Error {
  constructor() {
    super("Steam's library is still loading.");
  }
}

function materializedStore(): MaterializedAppStore | undefined {
  return window.appStore as unknown as MaterializedAppStore | undefined;
}

export function discoverGames(
  configuration: Configuration,
  store: MaterializedAppStore | undefined = materializedStore(),
): Game[] {
  if (!store?.m_bIsInitialized || !Array.isArray(store.allApps)) {
    throw new SteamLibraryLoadingError();
  }

  const games = new Map<string, Game>();
  for (const app of store.allApps) {
    const shortcut = (app.app_type & APP_TYPE_SHORTCUT) !== 0;
    const game = (app.app_type & APP_TYPE_GAME) !== 0;
    const installed =
      app.local_per_client_data?.installed === true ||
      app.per_client_data?.some((client) => client.installed === true) === true;
    if (!app.visible_in_game_list || (!game && !shortcut) || !installed) continue;

    const appId = String(app.appid >>> 0);
    games.set(appId, {
      appId,
      name: app.display_name,
      shortcut,
      enabled: configuration.games[appId] !== undefined,
      running: normalizeDisplayState(overviewDisplayStatus(app)) !== "idle",
    });
  }

  for (const record of Object.values(configuration.games)) {
    if (!games.has(record.appId)) {
      games.set(record.appId, {
        appId: record.appId,
        name: record.name,
        shortcut: record.shortcut,
        enabled: true,
        running: false,
        missing: true,
        conflict: "Managed entry is no longer present in Steam's allApps collection.",
      });
    }
  }
  return [...games.values()].sort((left, right) => left.name.localeCompare(right.name));
}

export async function readLaunchValue(
  game: Game,
  bridge: SteamBridge = SteamClient,
  store: SteamDetailsStore | undefined = window.appDetailsStore,
): Promise<string> {
  const appId = Number(game.appId);
  let details = store?.GetAppDetails(appId) ?? null;
  if (!details) {
    const register = bridge.Apps.RegisterForAppDetails;
    if (!register) throw new Error(`Steam details are unavailable for ${game.name}.`);
    details = await new Promise<SteamDetails>((resolve, reject) => {
      let registration: void | Unregisterable;
      let settled = false;
      const timer = setTimeout(() => {
        if (settled) return;
        settled = true;
        registration?.unregister();
        reject(new Error(`Steam details did not load for ${game.name}.`));
      }, 5_000);
      const finish = (nextDetails: SteamDetails) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        registration?.unregister();
        resolve(nextDetails);
      };
      try {
        registration = register.call(bridge.Apps, appId, finish);
        if (settled) registration?.unregister();
      } catch (reason) {
        clearTimeout(timer);
        reject(reason);
      }
    });
  }
  return game.shortcut
    ? details.strShortcutLaunchOptions ?? ""
    : details.strLaunchOptions ?? "";
}

export function setLaunchValue(game: Game, value: string, bridge: SteamBridge = SteamClient): void {
  if (game.shortcut) bridge.Apps.SetShortcutLaunchOptions(Number(game.appId), value);
  else bridge.Apps.SetAppLaunchOptions(Number(game.appId), value);
}

export async function enableManagedGame(game: Game, bridge: SteamBridge = SteamClient): Promise<void> {
  const original = await readLaunchValue(game, bridge);
  const managed = await enableGame(game.appId, game.name, game.shortcut, original);
  try {
    setLaunchValue(game, managed.managedLaunch, bridge);
  } catch (error) {
    await disableGame(game.appId, original);
    throw error;
  }
}

export async function disableManagedGame(game: Game, bridge: SteamBridge = SteamClient): Promise<string | undefined> {
  const config = await getConfiguration();
  const record = config.games[game.appId];
  if (!record) return undefined;
  if (game.missing) {
    const result = await disableGame(game.appId, record.managedLaunch);
    return result.message;
  }
  const current = await readLaunchValue(game, bridge);
  if (current !== record.managedLaunch && current !== record.originalLaunch) {
    const result = await disableGame(game.appId, current);
    return result.message ?? "Steam launch options were edited; your value was preserved.";
  }
  setLaunchValue(game, record.originalLaunch, bridge);
  try {
    const result = await disableGame(game.appId, record.managedLaunch);
    if (result.conflict) {
      setLaunchValue(game, current, bridge);
      return result.message;
    }
  } catch (error) {
    setLaunchValue(game, record.managedLaunch, bridge);
    throw error;
  }
  return undefined;
}

export function displayState(appId: string): "idle" | "launching" | "running" | "stopping" {
  const overview = materializedStore()?.GetAppOverviewByAppID(Number(appId));
  return normalizeDisplayState(overview ? overviewDisplayStatus(overview) : undefined);
}

function overviewDisplayStatus(overview: MaterializedAppOverview): number | undefined {
  return (
    overview.local_per_client_data?.display_status ??
    overview.most_available_per_client_data?.display_status ??
    overview.per_client_data?.find((client) => client.display_status !== undefined)?.display_status
  );
}

function normalizeDisplayState(status: number | undefined): "idle" | "launching" | "running" | "stopping" {
  if (status === 1) return "launching";
  if (status === 4) return "running";
  if (status === 36) return "stopping";
  return "idle";
}

export interface LifetimeObserverOptions {
  bridge?: SteamBridge;
  onError?(error: Error): void;
  sendLifetime?: typeof postLifetime;
  schedule?: (callback: () => void, milliseconds: number) => ReturnType<typeof setTimeout>;
  cancel?: (handle: ReturnType<typeof setTimeout>) => void;
}

export function observeSteamLifetime(options: LifetimeObserverOptions = {}): () => void {
  const bridge = options.bridge ?? SteamClient;
  const send = options.sendLifetime ?? postLifetime;
  const schedule = options.schedule ?? setTimeout;
  const cancel = options.cancel ?? clearTimeout;
  let active = true;
  const timers = new Set<ReturnType<typeof setTimeout>>();
  const lifetimeRegistration = bridge.GameSessions.RegisterForAppLifetimeNotifications((notification) => {
    const forward = (attempt: number) => {
      if (!active) return;
      void send(String(notification.unAppID), notification.nInstanceID, notification.bRunning)
        .then((result) => {
          if (active && notification.unAppID === 0 && result?.status === "unresolved" && attempt < 3) {
            const handle = schedule(() => {
              timers.delete(handle);
              forward(attempt + 1);
            }, 250 * 2 ** attempt);
            timers.add(handle);
          }
        })
        .catch((reason: unknown) => {
          if (active) options.onError?.(reason instanceof Error ? reason : new Error(String(reason)));
        });
    };
    forward(0);
  });
  return () => {
    active = false;
    lifetimeRegistration.unregister();
    for (const timer of timers) cancel(timer);
    timers.clear();
  };
}

export function observeSteamOverviews(
  onOverview: () => void,
  bridge: SteamBridge = SteamClient,
): () => void {
  const registration = bridge.Apps.RegisterForAppOverviewChanges?.(() => onOverview());
  return () => registration?.unregister();
}
