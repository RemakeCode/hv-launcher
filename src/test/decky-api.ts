export function fetchNoCors(input: string, init?: RequestInit): Promise<Response> {
  return fetch(input, init);
}
