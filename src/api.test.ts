import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  BASE_URL,
  BackendRequestError,
  applyUMIPConfiguration,
  getUMIPInspection,
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

  it("sends the selected path and destination with source confirmation", async () => {
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

    const path = "/home/deck/Downloads/GE-Proton11-1-LinUwUx.tar.xz";
    await expect(installProtonArchive(path, "native")).resolves.toEqual(response);
    expect(fetchMock).toHaveBeenCalledOnce();
    expect(fetchMock).toHaveBeenCalledWith(
      `${BASE_URL}/setup/proton/install`,
      {
        method: "POST",
        body: JSON.stringify({
          path,
          destinationId: "native",
          confirmedSource: true,
        }),
        headers: { "Content-Type": "application/json" },
      },
    );
  });

  it("preserves a Proton installation failure from the backend", async () => {
    fetchMock.mockResolvedValue(
      new Response('{"error":"open selected archive: file not found"}', {
        status: 422,
      }),
    );

    await expect(installProtonArchive("/home/deck/missing.tar.xz", "native")).rejects.toEqual(
      new BackendRequestError(
        "open selected archive: file not found",
        422,
      ),
    );
  });
});

describe("UMIP setup API", () => {
  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
  });
  afterEach(() => vi.unstubAllGlobals());

  it("uses the fixed inspection endpoint", async () => {
    const inspection = {
      liveUmip: true,
      selection: "automatic",
      selected: "limine",
      candidates: [],
      manual: [],
    };
    fetchMock.mockResolvedValueOnce(new Response(JSON.stringify(inspection)));
    await expect(getUMIPInspection()).resolves.toEqual(inspection);
    expect(fetchMock).toHaveBeenLastCalledWith(`${BASE_URL}/setup/umip`, {
      method: "GET",
      headers: { "Content-Type": "application/json" },
    });

  });

  it("submits only the selected bootloader and capability", async () => {
    const job = {
      id: "job-umip",
      kind: "umip-apply",
      state: "running",
      phase: "starting",
      progress: 0,
      output: [],
      startedAt: "2026-07-19T12:00:00Z",
    };
    fetchMock.mockResolvedValue(new Response(JSON.stringify(job)));

    await expect(applyUMIPConfiguration("grub", "signed-capability")).resolves.toEqual(job);
    expect(fetchMock).toHaveBeenCalledWith(`${BASE_URL}/setup/umip`, {
      method: "POST",
      body: JSON.stringify({
        bootloader: "grub",
        capability: "signed-capability",
      }),
      headers: { "Content-Type": "application/json" },
    });
  });
});
