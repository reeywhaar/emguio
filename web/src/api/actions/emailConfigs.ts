import { query, request } from "@app/api/transport";
import type {
  EmailConfig,
  EmailConfigDraft,
  Mailbox,
  MessagePage,
  ReadMessage,
  TestResult,
} from "@app/api/types";

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

/** Asks for a look at every mailbox now. What it finds arrives on the event stream. */
export const postEmailConfigsByIdSync = (id: string) =>
  request<void>(`/api/email-configs/${encodeURIComponent(id)}/sync`, {
    method: "POST",
  });

export const getEmailConfigsByIdMailboxes = (id: string) =>
  request<{ mailboxes: Mailbox[] }>(
    `/api/email-configs/${encodeURIComponent(id)}/mailboxes`,
  ).then((it) => it.mailboxes);

export const getEmailConfigsByIdMailboxesByMailboxMessages = (
  id: string,
  mailbox: string,
  cursor: string,
) =>
  request<MessagePage>(
    `/api/email-configs/${encodeURIComponent(id)}/mailboxes/${encodeURIComponent(mailbox)}/messages${query({ cursor })}`,
  );

export const getEmailConfigsByIdMessagesByMessage = (
  id: string,
  message: string,
  images: boolean,
) =>
  request<ReadMessage>(
    `/api/email-configs/${encodeURIComponent(id)}/messages/${encodeURIComponent(message)}${query({ images: images ? 1 : undefined })}`,
  );

/** Where a part of a message is downloaded from. */
export const partURL = (id: string, message: string, index: number) =>
  `/api/email-configs/${encodeURIComponent(id)}/messages/${encodeURIComponent(message)}/parts/${index}`;
