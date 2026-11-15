import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, request } from "@app/api/transport";

afterEach(() => vi.unstubAllGlobals());

const refused = (fetch: () => Promise<Response>) => {
  vi.stubGlobal("fetch", fetch);
  return request("/api/anything").then(
    () => {
      throw new Error("it was not refused");
    },
    (err: unknown) => err as ApiError,
  );
};

describe("a request that fails", () => {
  it("says the server could not be reached when there is no answer", async () => {
    const err = await refused(() =>
      Promise.reject(new TypeError("Failed to fetch")),
    );
    expect(err.code).toBe("unreachable");
    expect(err.message).toMatch(/could not be reached/);
  });

  it("says the server is not answering when a proxy answers for it", async () => {
    const err = await refused(
      async () => new Response("<html>Bad Gateway</html>", { status: 502 }),
    );
    expect(err.code).toBe("unreachable");
    expect(err.message).toMatch(/not answering/);
  });

  it("carries the server's own sentence", async () => {
    const err = await refused(
      async () =>
        new Response(
          JSON.stringify({ code: "invalid", message: "No such folder." }),
          { status: 400 },
        ),
    );
    expect(err.status).toBe(400);
    expect(err.message).toBe("No such folder.");
  });

  it("passes an abort through as it is", async () => {
    const stop = new AbortController();
    stop.abort();
    vi.stubGlobal("fetch", () =>
      Promise.reject(new DOMException("aborted", "AbortError")),
    );
    await expect(
      request("/api/anything", { signal: stop.signal }),
    ).rejects.toThrow("aborted");
  });
});
