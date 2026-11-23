import { request, upload } from "@app/api/transport";
import type { Import } from "@app/api/types";

/** What a file is sent as, by its name: an mbox, one eml, or a zip of them. */
export function kindOf(name: string): string | null {
  const lower = name.toLowerCase();
  if (lower.endsWith(".mbox")) return "application/mbox";
  if (lower.endsWith(".eml")) return "message/rfc822";
  if (lower.endsWith(".zip")) return "application/zip";
  return null;
}

/** Uploads a file to import into a folder, saying how much has gone; the import goes on after. */
export const postEmailConfigsByIdMailboxesByMailboxImport = (
  id: string,
  mailbox: string,
  file: File,
  kind: string,
  onProgress: (sent: number, total: number) => void,
) =>
  upload<Import>(
    `/api/email-configs/${encodeURIComponent(id)}/mailboxes/${encodeURIComponent(mailbox)}/import?name=${encodeURIComponent(file.name)}`,
    file,
    kind,
    onProgress,
  );

/** The user's imports, running and finished, oldest first. */
export const getImports = () =>
  request<{ imports: Import[] }>("/api/imports").then((it) => it.imports);

/** Stops an import; what went in stays. */
export const postImportsByIdCancel = (id: string) =>
  request<Import>(`/api/imports/${encodeURIComponent(id)}/cancel`, {
    method: "POST",
  });

/** Lets a finished import go, once it has been shown. */
export const deleteImportsById = (id: string) =>
  request<void>(`/api/imports/${encodeURIComponent(id)}`, { method: "DELETE" });
