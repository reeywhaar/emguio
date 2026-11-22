import { request } from "@app/api/transport";
import type { Session } from "@app/api/types";

/** The user's sessions, the most recently used first. */
export const getSessions = () =>
  request<{ sessions: Session[] }>("/api/sessions").then((it) => it.sessions);

/** Signs one session out; the current one too, which clears its cookie. */
export const deleteSessionsById = (id: string) =>
  request<void>(`/api/sessions/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });

/** Signs out every session but this one. */
export const deleteSessions = () =>
  request<void>("/api/sessions", { method: "DELETE" });
