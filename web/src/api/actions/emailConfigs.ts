import { query, request } from "@app/api/transport";
import type {
  EmailConfig,
  EmailConfigDraft,
  ClosedDraft,
  Conversation,
  KeptDraft,
  Mailbox,
  MessagePage,
  Outgoing,
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

/** Sends a message, and answers once the outgoing server has taken it. */
export const postEmailConfigsByIdSend = (id: string, body: Outgoing) =>
  request<{ message_id: string }>(
    `/api/email-configs/${encodeURIComponent(id)}/send`,
    { method: "POST", body },
  );

/** Keeps a new draft in emguio, written to Drafts in a while; with close, now. */
export const postEmailConfigsByIdDrafts = <T extends KeptDraft | ClosedDraft>(
  id: string,
  body: Outgoing,
) =>
  request<T>(`/api/email-configs/${encodeURIComponent(id)}/drafts`, {
    method: "POST",
    body,
  });

/** Keeps what is written in a draft now. */
export const putEmailConfigsByIdDraftsByDraft = <
  T extends KeptDraft | ClosedDraft,
>(
  id: string,
  draft: string,
  body: Outgoing,
) =>
  request<T>(
    `/api/email-configs/${encodeURIComponent(id)}/drafts/${encodeURIComponent(draft)}`,
    { method: "PUT", body },
  );

/** Discards a draft kept in emguio, and says where the mail server holds its copy. */
export const deleteEmailConfigsByIdDraftsByDraft = (
  id: string,
  draft: string,
) =>
  request<{ kept: { mailbox: string; message: string } | null }>(
    `/api/email-configs/${encodeURIComponent(id)}/drafts/${encodeURIComponent(draft)}`,
    { method: "DELETE" },
  );

export const getEmailConfigsByIdMailboxes = (id: string) =>
  request<{ mailboxes: Mailbox[] }>(
    `/api/email-configs/${encodeURIComponent(id)}/mailboxes`,
  ).then((it) => it.mailboxes);

export const getEmailConfigsByIdMailboxesByMailboxMessages = (
  id: string,
  mailbox: string,
  cursor: string,
  q = "",
) =>
  request<MessagePage>(
    `/api/email-configs/${encodeURIComponent(id)}/mailboxes/${encodeURIComponent(mailbox)}/messages${query({ cursor, q })}`,
  );

const messagePath = (id: string, mailbox: string, message: string) =>
  `/api/email-configs/${encodeURIComponent(id)}/mailboxes/${encodeURIComponent(mailbox)}/messages/${encodeURIComponent(message)}`;

/** A message whole, from the server: nothing of it is kept, so every opening asks. */
export const getEmailConfigsByIdMailboxesByMailboxMessagesByMessage = (
  id: string,
  mailbox: string,
  message: string,
) => request<ReadMessage>(messagePath(id, mailbox, message));

/** The conversation a message is in. */
export const getEmailConfigsByIdMailboxesByMailboxMessagesByMessageConversation =
  (id: string, mailbox: string, message: string) =>
    request<Conversation>(`${messagePath(id, mailbox, message)}/conversation`);

/** How many messages each of some is in a conversation with in its folder, those not alone. */
export const getEmailConfigsByIdMailboxesByMailboxThreads = (
  id: string,
  mailbox: string,
  messages: string[],
) =>
  request<{ counts: Record<string, number> }>(
    `/api/email-configs/${encodeURIComponent(id)}/mailboxes/${encodeURIComponent(mailbox)}/threads${query({ messages: messages.join(",") })}`,
  );

/** Where a part of a message is downloaded from, by its section. */
export const partURL = (
  id: string,
  mailbox: string,
  message: string,
  section: string,
) => `${messagePath(id, mailbox, message)}/parts/${section}`;
