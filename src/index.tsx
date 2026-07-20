import { definePlugin, routerHook, toaster } from "@decky/api";
import { staticClasses } from "@decky/ui";
import { VscVmRunning } from "react-icons/vsc";
import { ReadinessContent, MANAGEMENT_ROUTE, READINESS_ROUTE } from "./qam/qam";
import { ReadinessWorkspace } from "./readiness-workspace/readiness-workspace";
import { ShortcutManagementPage } from "./shortcut-management/shortcut-management-page";
import { setupEventStore } from "./setup-events";
import { logger } from "./shared/logger";
import { observeSteamLifetime } from "./steam";

export default definePlugin(() => {
  routerHook.addRoute(MANAGEMENT_ROUTE, ShortcutManagementPage);
  routerHook.addRoute(READINESS_ROUTE, ReadinessWorkspace);

  setupEventStore.start((job) => {
    const succeeded = job.state === "succeeded";
    const umip = job.kind === "umip-apply";
    toaster.toast({
      title: succeeded ? "HV Launcher setup complete" : "HV Launcher setup failed",
      body: succeeded
        ? umip
          ? "The boot configuration was updated. Restart the system to finish disabling UMIP."
          : "The Proton installation finished. Restart Steam before selecting the new tool."
        : umip
          ? "The UMIP configuration did not complete. Open Readiness setup for recovery details."
          : "The Proton installation did not complete. Open Readiness setup for details.",
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
