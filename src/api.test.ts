import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  BASE_URL,
  BackendRequestError,
  getStatus,
  installProtonArchive,
  preflightProtonArchive,
} from "./api";

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

describe("Proton setup API", () => {
  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => vi.unstubAllGlobals());

  it("sends the selected path to the fixed preflight endpoint", async () => {
    const response = {
      selectionId: "selection-1",
      expiresAt: "2026-07-19T12:01:00Z",
      responsibility: "Confirm that you selected the intended archive.",
      preflight: {
        fileName: "GE-Proton11-1-LinUwUx.tar.xz",
        compression: "xz",
        compressedBytes: 1024,
        destinations: [{ id: "native", label: "Steam" }],
      },
    };
    fetchMock.mockResolvedValue(new Response(JSON.stringify(response)));

    await expect(
      preflightProtonArchive("/home/deck/Downloads/GE-Proton11-1-LinUwUx.tar.xz"),
    ).resolves.toEqual(response);
    expect(fetchMock).toHaveBeenCalledOnce();
    expect(fetchMock).toHaveBeenCalledWith(
      `${BASE_URL}/setup/proton/preflight`,
      {
        method: "POST",
        body: JSON.stringify({
          path: "/home/deck/Downloads/GE-Proton11-1-LinUwUx.tar.xz",
        }),
        headers: { "Content-Type": "application/json" },
      },
    );
  });

  it("binds installation to the selected record and destination with source confirmation", async () => {
    const response = {
      id: "job-1",
      kind: "proton-install",
      state: "running",
      phase: "opening-archive",
      progress: 5,
      output: [],
      startedAt: "2026-07-19T12:00:00Z",
    };
    fetchMock.mockResolvedValue(new Response(JSON.stringify(response)));

    await expect(installProtonArchive("selection-1", "native")).resolves.toEqual(response);
    expect(fetchMock).toHaveBeenCalledOnce();
    expect(fetchMock).toHaveBeenCalledWith(
      `${BASE_URL}/setup/proton/install`,
      {
        method: "POST",
        body: JSON.stringify({
          selectionId: "selection-1",
          destinationId: "native",
          confirmedSource: true,
        }),
        headers: { "Content-Type": "application/json" },
      },
    );
  });

  it("preserves a Proton installation failure from the backend", async () => {
    fetchMock.mockResolvedValue(
      new Response('{"error":"The selected archive has expired; choose it again."}', {
        status: 410,
      }),
    );

    await expect(installProtonArchive("expired", "native")).rejects.toEqual(
      new BackendRequestError(
        "The selected archive has expired; choose it again.",
        410,
      ),
    );
  });
});
