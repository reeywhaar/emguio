import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { getEmailConfigsByIdMailboxes } from "@app/api/actions/emailConfigs";
import {
  deleteExportsById,
  getExports,
  postEmailConfigsByIdMailboxesByMailboxExport,
  postExportsByIdDelete,
} from "@app/api/actions/exports";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { Export } from "@app/api/types";
import { Button } from "@app/components/Button";
import { useConfirm } from "@app/components/Confirm";
import { Cross } from "@app/components/icons";
import { download } from "@app/download";
import { useReadOn } from "@app/islands/app/Actions";
import { within } from "@app/islands/app/mailbox";
import { go, paths } from "@app/islands/app/route";
import { say } from "@app/islands/app/notices";

/** How often exports are read while one downloads. */
const POLL = 1000;

const messages = (n: number) => (n === 1 ? "1 message" : `${n} messages`);

/**
 * Exports a folder, its subfolders with it, or a selection of it, as soon as asked: the download
 * starts, and its card in the corner says how far it has got. See docs/reading.md.
 */
export function useExportMail(config: string) {
  const client = useQueryClient();
  return useMutation({
    mutationFn: ({
      mailbox,
      ...body
    }: {
      mailbox: string;
      format: "mbox" | "eml";
      messages: string[];
      subfolders: boolean;
    }) => postEmailConfigsByIdMailboxesByMailboxExport(config, mailbox, body),
    onSuccess: (x) => {
      client.setQueryData<Export[]>(qk.exports, (old) => [...(old ?? []), x]);
      download(x.url, x.file);
      void client.invalidateQueries({ queryKey: qk.exports });
    },
    onError: (err) => say(messageOf(err)),
  });
}

/**
 * Downloads of mail, in the bottom-right corner: how far each has got, then once it is done,
 * deleting what went into it, or letting it go. See docs/reading.md.
 */
export function Exports() {
  const client = useQueryClient();
  const exports = useQuery({
    queryKey: qk.exports,
    queryFn: getExports,
    refetchInterval: (q) =>
      (q.state.data ?? []).some(
        (x) => x.state === "ready" || x.state === "exporting",
      )
        ? POLL
        : false,
  });
  const dismiss = useMutation({
    mutationFn: deleteExportsById,
    onMutate: (id) =>
      client.setQueryData<Export[]>(qk.exports, (old) =>
        old?.filter((x) => x.id !== id),
      ),
  });
  const shown = (exports.data ?? []).filter((x) => x.state !== "deleted");
  if (shown.length === 0) return null;
  return (
    <div className="flex flex-col gap-2">
      {shown.map((x) => (
        <Card key={x.id} x={x} onDismiss={() => dismiss.mutate(x.id)} />
      ))}
    </div>
  );
}

function Card({ x, onDismiss }: { x: Export; onDismiss: () => void }) {
  const client = useQueryClient();
  const confirm = useConfirm();
  const readOn = useReadOn(x.email_config);
  const boxes = useQuery({
    queryKey: qk.mailboxes(x.email_config),
    queryFn: () => getEmailConfigsByIdMailboxes(x.email_config),
  });
  const folder = boxes.data?.find((mb) => mb.id === x.mailbox);
  // To Trash, as deleting does; for good from Trash, or where there is none.
  const toTrash =
    folder?.special_use !== "trash" &&
    (boxes.data ?? []).some(
      (mb) => mb.special_use === "trash" && mb.selectable,
    );
  const remove = useMutation({
    mutationFn: () =>
      postExportsByIdDelete(x.id, { read: toTrash && readOn.trash }),
    onSuccess: ({ trash, deleted, folders, kept }) => {
      const mail =
        deleted === 0
          ? `Moved ${messages(trash)} to Trash`
          : trash === 0
            ? `Deleted ${messages(deleted)} for good`
            : `Moved ${messages(trash)} to Trash and deleted ${messages(deleted)} for good`;
      const gone =
        folders === 0
          ? ""
          : folders === 1
            ? ", and the folder"
            : `, and ${folders} folders`;
      const stays =
        kept === 0
          ? ""
          : kept === 1
            ? " One folder stays: mail came into it since."
            : ` ${kept} folders stay: mail came into them since.`;
      say(`${mail}${gone}.${stays}`);
      // Out of a folder that is gone.
      const tree = [
        x.mailbox,
        ...(boxes.data ?? [])
          .filter((mb) => folder && within(mb, folder))
          .map((mb) => mb.id),
      ];
      if (
        folders > 0 &&
        kept === 0 &&
        tree.some((id) =>
          window.location.pathname.startsWith(paths.mail(x.email_config, id)),
        )
      ) {
        go(paths.mail(x.email_config));
      }
      void client.invalidateQueries({ queryKey: qk.lists(x.email_config) });
      void client.invalidateQueries({
        queryKey: qk.mailboxes(x.email_config),
      });
      onDismiss();
    },
    onError: (err) => say(messageOf(err)),
  });
  const deleteThem = async () => {
    // Moved to Trash is put back from there; deleted for good is not, so that one asks.
    if (
      !toTrash &&
      !(await confirm({
        title: `Delete the ${messages(x.exported)} downloaded?`,
        message:
          "They are deleted from the server for good. Mail that arrived since stays. This cannot be undone.",
        confirm: "Delete",
        danger: true,
      }))
    ) {
      return;
    }
    remove.mutate();
  };

  if (x.state === "ready" || x.state === "exporting") {
    const total = Math.max(x.total, x.exported);
    return (
      <div
        role="status"
        className="flex flex-col gap-2 rounded-lg border border-line bg-bg p-3 text-sm shadow-lg"
      >
        <p className="truncate">Downloading {x.file}</p>
        <div
          aria-hidden="true"
          className="h-1 overflow-hidden rounded-full bg-fill"
        >
          <div
            className="h-full rounded-full bg-brand transition-[width]"
            style={{ width: `${(100 * x.exported) / Math.max(total, 1)}%` }}
          />
        </div>
        <span className="text-xs text-muted tabular-nums">
          {x.exported} of {total}
        </span>
      </div>
    );
  }
  if (x.state === "done" && x.exported > 0) {
    return (
      <div
        role="status"
        className="flex flex-col gap-2 rounded-lg border border-line bg-bg p-3 text-sm shadow-lg"
      >
        <p className="min-w-0 break-words">{x.file} downloaded.</p>
        <div className="flex justify-end gap-2">
          <Button
            size="bar"
            variant="danger"
            disabled={remove.isPending || !boxes.data}
            onClick={() => void deleteThem()}
          >
            Delete downloaded
          </Button>
          <Button size="bar" onClick={onDismiss}>
            Cancel
          </Button>
        </div>
      </div>
    );
  }
  return (
    <div
      role="status"
      className="flex items-start gap-2 rounded-lg border border-line bg-bg p-3 text-sm shadow-lg"
    >
      <p className="min-w-0 flex-1">
        {x.state === "failed" ? (
          <span className="text-accent">{x.error}</span>
        ) : (
          `${x.file} downloaded: nothing was in it.`
        )}
      </p>
      <button
        type="button"
        aria-label="Dismiss"
        onClick={onDismiss}
        className="-m-1 shrink-0 rounded p-1 text-faint hover:bg-fill hover:text-fg"
      >
        <Cross />
      </button>
    </div>
  );
}
