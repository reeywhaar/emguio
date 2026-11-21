import { request } from "@app/api/transport";

/** Opens the user's seal key until the server stops. */
export const postSealUnseal = (key: string) =>
  request<void>("/api/seal/unseal", { method: "POST", body: { key } });

/** Makes the first seal key of a user from before seal keys. */
export const postSeal = (key: string) =>
  request<void>("/api/seal", { method: "POST", body: { key } });

/**
 * Makes the first seal key of a user from before seal keys out of the key the server ran with,
 * keeping their saved mail passwords.
 */
export const postSealCarry = (key: string) =>
  request<void>("/api/seal/carry", { method: "POST", body: { key } });

/** Changes the seal key; the current one has to open it. */
export const putSeal = (current: string, key: string) =>
  request<void>("/api/seal", { method: "PUT", body: { current, key } });

/** Replaces a forgotten seal key: the saved mail passwords are asked for again. */
export const postSealReset = (key: string) =>
  request<void>("/api/seal/reset", { method: "POST", body: { key } });
