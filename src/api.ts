import { Fetcher, FetcherError } from "./shared/fetcher";
import type {
  Configuration,
  ManageResponse,
  RestoreResponse,
  SystemStatus,
} from "./types";

export const BASE_URL = "http://127.0.0.1:42991/v1";
export { FetcherError as BackendRequestError };

const fetcher = new Fetcher(BASE_URL);

export const getStatus = () => fetcher.get<SystemStatus>("/status");
export const getConfiguration = () => fetcher.get<Configuration>("/config");
export const enableGame = (
  appId: string,
  name: string,
  shortcut: boolean,
  currentLaunch: string,
) => fetcher.post<ManageResponse>(`/games/${appId}/enable`, {
  name,
  shortcut,
  currentLaunch,
});
export const disableGame = (appId: string, currentLaunch: string) =>
  fetcher.post<RestoreResponse>(`/games/${appId}/disable`, { currentLaunch });
export const postLifetime = (appId: string, instanceId: number, running: boolean) =>
  fetcher.post<{ status?: string } | undefined>("/lifetime", {
    appId,
    instanceId,
    running,
  });
