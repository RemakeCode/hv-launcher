export async function fetchNoCors(): Promise<Response> {
  throw new Error("Decky HTTP is not available in unit tests");
}
