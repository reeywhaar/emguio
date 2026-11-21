/**
 * The only file that mentions fetch.
 *
 * credentials is same-origin and there is no other origin: a request needing CORS would be a
 * bug rather than a feature.
 */

/** A refusal, carrying the server's own code and sentence. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

type Options = {
  method?: string;
  body?: unknown;
  signal?: AbortSignal;
};

/** Told when a request is refused because the user is sealed; see whenSealed. */
let sealed: (() => void) | null = null;

/** Has fn told of a request refused as sealed, until what it returns is called. */
export function whenSealed(fn: () => void): () => void {
  sealed = fn;
  return () => {
    if (sealed === fn) sealed = null;
  };
}

export async function request<T>(
  path: string,
  options: Options = {},
): Promise<T> {
  const { method = "GET", body, signal } = options;

  const init: RequestInit = { method, credentials: "same-origin", signal };
  if (body !== undefined) {
    init.headers = { "Content-Type": "application/json" };
    init.body = JSON.stringify(body);
  }

  let response: Response;
  try {
    response = await fetch(path, init);
  } catch (err) {
    // An abort is the caller's own.
    if (signal?.aborted) throw err;
    throw new ApiError(
      0,
      "unreachable",
      "The server could not be reached. Check your connection and try again.",
    );
  }
  if (response.status === 204) return undefined as T;

  const text = await response.text();
  if (!response.ok) {
    // The server writes the sentence for whoever reads it; re-wording it here would mean
    // maintaining two vocabularies for one failure.
    const refusal = refusalOf(text);
    if (refusal?.code === "sealed") sealed?.();
    if (refusal) {
      throw new ApiError(
        response.status,
        refusal.code ?? "invalid",
        refusal.message ?? "Something went wrong here.",
      );
    }
    // No sentence of emguio's: a proxy in front answered for it.
    if (gateway.has(response.status)) {
      throw new ApiError(
        response.status,
        "unreachable",
        "The server is not answering. Try again in a moment.",
      );
    }
    throw new ApiError(
      response.status,
      "invalid",
      "Something went wrong here.",
    );
  }
  return (text ? JSON.parse(text) : undefined) as T;
}

/** What a proxy answers with when what is behind it is down or slow. */
const gateway = new Set([502, 503, 504]);

/** emguio's own refusal, or nothing when the body is not one: a proxy's page, or empty. */
function refusalOf(
  text: string,
): { code?: string; message?: string } | undefined {
  try {
    const parsed: unknown = JSON.parse(text);
    return typeof parsed === "object" && parsed !== null
      ? (parsed as { code?: string; message?: string })
      : undefined;
  } catch {
    return undefined;
  }
}

/** The sentence to show for anything a request threw. */
export function messageOf(err: unknown): string {
  return err instanceof ApiError ? err.message : "Something went wrong here.";
}

/** Query strings, with empty values left out rather than sent as blanks. */
export function query(
  params: Record<string, string | number | undefined>,
): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") search.set(key, String(value));
  }
  const rendered = search.toString();
  return rendered ? `?${rendered}` : "";
}
