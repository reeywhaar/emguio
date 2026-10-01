import { request } from "@app/api/transport";
import type { EmailConfig, EmailConfigDraft, TestResult } from "@app/api/types";

/** Named mechanically from the route, so a call site and a handler find each other by grep. */

export const getEmailConfigs = () =>
  request<{ email_configs: EmailConfig[] }>("/api/email-configs").then(
    (it) => it.email_configs,
  );

export const postEmailConfigs = (body: EmailConfigDraft) =>
  request<EmailConfig>("/api/email-configs", { method: "POST", body });

export const putEmailConfigsById = (id: string, body: EmailConfigDraft) =>
  request<EmailConfig>(`/api/email-configs/${encodeURIComponent(id)}`, {
    method: "PUT",
    body,
  });

export const deleteEmailConfigsById = (id: string) =>
  request<void>(`/api/email-configs/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });

/** Signs in to a draft that is not saved yet. */
export const postEmailConfigsTest = (body: EmailConfigDraft) =>
  request<TestResult>("/api/email-configs/test", { method: "POST", body });

/** Signs in to a draft of a saved config, with its saved passwords where the draft has none. */
export const postEmailConfigsByIdTest = (id: string, body: EmailConfigDraft) =>
  request<TestResult>(`/api/email-configs/${encodeURIComponent(id)}/test`, {
    method: "POST",
    body,
  });
