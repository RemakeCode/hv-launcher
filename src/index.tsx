import { definePlugin, routerHook } from "@decky/api";
import { staticClasses } from "@decky/ui";
import { FaMicrochip } from "react-icons/fa";
import { ShortcutManagementPage } from "./manage";
import { ReadinessContent, MANAGEMENT_ROUTE } from "./qam";
import { logger } from "./shared/logger";
import { observeSteamLifetime } from "./steam";

export default definePlugin(() => {
  routerHook.addRoute(MANAGEMENT_ROUTE, ShortcutManagementPage);

  const stopLifetimeObserver = observeSteamLifetime({
    onError: (reason) => logger.error("Failed to forward a Steam lifetime notification", reason),
  });

  return {
    name: "HV Launcher",
    titleView: <div className={staticClasses.Title}>HV Launcher</div>,
    content: <ReadinessContent />,
    icon: <FaMicrochip />,
    onDismount: () => {
      stopLifetimeObserver();
      routerHook.removeRoute(MANAGEMENT_ROUTE);
    },
  };
});
