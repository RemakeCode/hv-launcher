import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { BackendRequestError, getStatus } from "./api";

const fetchMock = vi.fn<typeof fetch>();

describe("backend errors", () => {
  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => vi.unstubAllGlobals());

  it("extracts the backend JSON error and status", async () => {
    fetchMock.mockResolvedValue(new Response('{"error":"KVM is busy"}', { status: 423 }));

    await expect(getStatus()).rejects.toEqual(
      new BackendRequestError("KVM is busy", 423),
    );
  });
});
