import { callable } from "@decky/api";

export type PrivilegedSetupOperation =
  | "umip-apply"
  | "module-preflight"
  | "module-install";

const requestSetupCapability = callable<[PrivilegedSetupOperation, string], string>(
  "issue_setup_capability",
);

export function issueSetupCapability(
  operation: PrivilegedSetupOperation,
  binding: string,
): Promise<string> {
  return requestSetupCapability(operation, binding);
}
