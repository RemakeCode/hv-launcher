import { Fetcher, FetcherError } from "./shared/fetcher";
import type {
  Configuration,
  ManageResponse,
  ActiveSetupJob,
  ProtonPreflightResponse,
  SetupJobSnapshot,
  SystemStatus,
  UMIPBootloader,
  UMIPInspection,
  ModulePreflight,
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
export const disableGame = (appId: string) =>
  fetcher.post<void>(`/games/${appId}/disable`, {});
export const postLifetime = (appId: string, instanceId: number, running: boolean) =>
  fetcher.post<{ status?: string } | undefined>("/lifetime", {
    appId,
    instanceId,
    running,
  });

export const preflightProtonArchive = (path: string) =>
  fetcher.post<ProtonPreflightResponse>("/setup/proton/preflight", { path });

export const installProtonArchive = (
  path: string,
  destinationId: string,
) => fetcher.post<SetupJobSnapshot>("/setup/proton/install", {
  path,
  destinationId,
  confirmedSource: true,
});

export const getUMIPInspection = () =>
  fetcher.get<UMIPInspection>("/setup/umip");

export const getModulePreflight = () =>
  fetcher.get<ModulePreflight>("/setup/module/preflight");

export const installModuleArchive = (path: string, capability: string) =>
  fetcher.post<SetupJobSnapshot>("/setup/module/install", {
    path,
    capability,
  });

export const applyUMIPConfiguration = (
  bootloader: UMIPBootloader,
  capability: string,
) => fetcher.post<SetupJobSnapshot>("/setup/umip", {
  bootloader,
  capability,
});

export const getActiveSetupJob = () =>
  fetcher.get<ActiveSetupJob>("/setup/jobs/active");

export const getSetupJob = (jobId: string) =>
  fetcher.get<SetupJobSnapshot>(`/setup/jobs/${encodeURIComponent(jobId)}`);
