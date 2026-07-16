type Transport = typeof fetch;

export class FetcherError extends Error {
  constructor(
    message: string,
    readonly status?: number,
    options?: ErrorOptions,
  ) {
    super(message, options);
    this.name = "FetcherError";
  }
}

function errorMessage(value: unknown): string | undefined {
  if (!value || typeof value !== "object") return undefined;
  const body = value as { error?: unknown; message?: unknown };
  if (typeof body.error === "string") return body.error;
  if (typeof body.message === "string") return body.message;
  return undefined;
}

export class Fetcher {
  constructor(
    private readonly baseUrl = "",
    private readonly transport?: Transport,
  ) {}

  get<Type>(path: string): Promise<Type> {
    return this.request<Type>(path, { method: "GET" });
  }

  post<Type>(path: string, body: unknown): Promise<Type> {
    return this.request<Type>(path, { method: "POST", body: JSON.stringify(body) });
  }

  delete<Type = void>(path: string, body?: unknown): Promise<Type> {
    return this.request<Type>(path, {
      method: "DELETE",
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  }

  put<Type>(path: string, body: unknown): Promise<Type> {
    return this.request<Type>(path, { method: "PUT", body: JSON.stringify(body) });
  }

  patch<Type>(path: string, body?: unknown): Promise<Type> {
    return this.request<Type>(path, {
      method: "PATCH",
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  }

  private async request<Type>(path: string, init: RequestInit): Promise<Type> {
    let response: Response;
    try {
      response = await (this.transport ?? fetch)(this.url(path), {
        ...init,
        headers: { "Content-Type": "application/json", ...(init.headers ?? {}) },
      });
    } catch (cause) {
      throw new FetcherError(
        "The backend is unavailable. It may have failed to start or its port may already be in use; check the Decky Loader log.",
        undefined,
        { cause },
      );
    }

    const text = await response.text();
    if (!response.ok) {
      let parsed: unknown;
      try {
        parsed = text ? JSON.parse(text) : undefined;
      } catch {
        // A proxy or transport failure may return plain text.
      }
      throw new FetcherError(
        (errorMessage(parsed) ?? text) || `Backend returned ${response.status}`,
        response.status,
      );
    }
    if (response.status === 204 || !text) return undefined as Type;

    try {
      return JSON.parse(text) as Type;
    } catch (cause) {
      throw new FetcherError("The backend returned an invalid JSON response.", response.status, { cause });
    }
  }

  private url(path: string): string {
    return path.startsWith("http://") || path.startsWith("https://")
      ? path
      : `${this.baseUrl}${path}`;
  }
}
