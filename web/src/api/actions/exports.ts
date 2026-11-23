import { request } from "@app/api/transport";
import type { Export } from "@app/api/types";

/** Asks for a folder's mail, or a selection of it, as a file to download. */
export const postEmailConfigsByIdMailboxesByMailboxExport = (
  id: string,
  mailbox: string,
  body: { format: "mbox" | "eml"; messages: string[]; subfolders: boolean },
) =>
  request<Export>(
    `/api/email-configs/${encodeURIComponent(id)}/mailboxes/${encodeURIComponent(mailbox)}/export`,
    { method: "POST", body },
  );

/** How far an export has got. */
export const getExportsById = (id: string) =>
  request<Export>(`/api/exports/${encodeURIComponent(id)}`);

/** Deletes what went into a finished export: to Trash, or for good from Trash. */
export const postExportsByIdDelete = (id: string, body: { read: boolean }) =>
  request<{ trash: number; deleted: number }>(
    `/api/exports/${encodeURIComponent(id)}/delete`,
    { method: "POST", body },
  );
