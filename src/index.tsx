import { definePlugin, routerHook, toaster } from "@decky/api";
import { staticClasses } from "@decky/ui";
import { VscVmRunning } from "react-icons/vsc";
import { ReadinessContent, MANAGEMENT_ROUTE, READINESS_ROUTE } from "./qam/qam";
import { ReadinessWorkspace } from "./readiness-workspace/readiness-workspace";
import { ShortcutManagementPage } from "./shortcut-management/shortcut-management-page";
import { setupEventStore } from "./setup-events";
import { logger } from "./shared/logger";
import { observeSteamLifetime } from "./steam";
import type { SetupJobSnapshot } from "./types";

function setupToastBody(job: SetupJobSnapshot): string {
  const succeeded = job.state === "succeeded";

  switch (job.kind) {
    case "umip-apply":
      return succeeded
        ? "The boot configuration was updated. Restart the system to finish disabling UMIP."
        : "The UMIP configuration did not complete. Open Readiness setup for recovery details.";
    case "module-install":
      return succeeded
        ? "The CPUID module installation finished. Reopen Readiness setup to review signing status."
        : "The CPUID module installation did not complete. Open Readiness setup for details.";
    default:
      return succeeded
        ? "The Proton installation finished. Restart Steam before selecting the new tool."
        : "The Proton installation did not complete. Open Readiness setup for details.";
  }
}

export default definePlugin(() => {
  routerHook.addRoute(MANAGEMENT_ROUTE, ShortcutManagementPage);
  routerHook.addRoute(READINESS_ROUTE, ReadinessWorkspace);

  setupEventStore.start((job) => {
    const succeeded = job.state === "succeeded";
    toaster.toast({
      title: succeeded ? "HV Launcher setup complete" : "HV Launcher setup failed",
      body: setupToastBody(job),
      critical: !succeeded,
      playSound: true,
      showToast: true,
    });
  });

  const stopLifetimeObserver = observeSteamLifetime({
    onError: (reason) => logger.error("Failed to forward a Steam lifetime notification", reason),
  });

  return {
    name: "HV Launcher",
    titleView: <div className={staticClasses.Title}>HV Launcher</div>,
    content: <ReadinessContent />,
    icon: <VscVmRunning />,
    onDismount: () => {
      stopLifetimeObserver();
      setupEventStore.stop();
      routerHook.removeRoute(MANAGEMENT_ROUTE);
      routerHook.removeRoute(READINESS_ROUTE);
    },
  };
});
