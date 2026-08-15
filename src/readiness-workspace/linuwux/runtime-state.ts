import type { RuntimeSetupStatus } from '@/types';

export type RuntimeAction = "install" | "update" | "repair";

export function runtimeAction(setup?: RuntimeSetupStatus): RuntimeAction | undefined {
  const runtime = setup?.runtime;
  if (!runtime?.supported) return undefined;
  if (runtime.state === "absent") return "install";
  if (runtime.state === "invalid" || runtime.available && !runtime.versionKnown) return "repair";
  if (runtime.updateState === "update-available") return "update";
  return undefined;
}
