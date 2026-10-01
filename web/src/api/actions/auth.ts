import { request } from "@app/api/transport";
import type { Me } from "@app/api/types";

/** Named mechanically from the route, so a call site and a handler find each other by grep. */

export const postAuthLogin = (body: { username: string; password: string }) =>
  request<void>("/api/auth/login", { method: "POST", body });

export const postAuthLogout = () =>
  request<void>("/api/auth/logout", { method: "POST" });

export const getAuthMe = () => request<Me>("/api/auth/me");

export const getAuthInvitesByToken = (token: string) =>
  request<{ expires_at: number }>(
    `/api/auth/invites/${encodeURIComponent(token)}`,
  );

export const postAuthInvitesByTokenAccept = (
  token: string,
  body: { username: string; password: string },
) =>
  request<void>(`/api/auth/invites/${encodeURIComponent(token)}/accept`, {
    method: "POST",
    body,
  });
