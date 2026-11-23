import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  getExportsById,
  postEmailConfigsByIdMailboxesByMailboxExport,
  postExportsByIdDelete,
} from "@app/api/actions/exports";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { Mailbox } from "@app/api/types";
import { Button } from "@app/components/Button";
import { Choice } from "@app/components/Choice";
import { useConfirm } from "@app/components/Confirm";
import { Dialog } from "@app/components/Dialog";
import { Select } from "@app/components/Field";
import { Switch } from "@app/components/Switch";
import { download } from "@app/download";
import { useReadOn } from "@app/islands/app/Actions";
import { labelOfMailbox, within } from "@app/islands/app/mailbox";
import { say } from "@app/islands/app/notices";

/** How often an export is asked how far it has got. */
const POLL = 1000;

const count = (n: number) => (n === 1 ? "1 message" : `${n} messages`);

/**
 * Exporting a folder, or a selection of it: the format and the subfolders, then the download as
 * it goes, and once it is done, deleting what went into it. See docs/reading.md.
 */
export function ExportDialog({
  config,
  folder,
  boxes,
  messages,
  open,
  onClose,
  onDeleted,
}: {
  config: string;
  folder: Mailbox;
  boxes: Mailbox[];
  /** A selection of the folder's messages; none exports all of it. */
  messages?: string[];
  open: boolean;
  onClose: () => void;
  /** Told once what was exported has been deleted. */
  onDeleted?: () => void;
}) {
  const client = useQueryClient();
  const confirm = useConfirm();
  const readOn = useReadOn(config);
  const [format, setFormat] = useState<"mbox" | "eml">("mbox");
  const [subfolders, setSubfolders] = useState(false);
  const [id, setId] = useState("");
  const name = labelOfMailbox(folder);
  const inside = boxes.filter((mb) => within(mb, folder)).length;
  const one = messages?.length === 1;

  const start = useMutation({
    mutationFn: () =>
      postEmailConfigsByIdMailboxesByMailboxExport(config, folder.id, {
        format,
        messages: messages ?? [],
        subfolders: !messages && subfolders,
      }),
    onSuccess: (x) => {
      client.setQueryData(qk.export(x.id), x);
      setId(x.id);
      download(x.url, x.file);
    },
  });
  const progress = useQuery({
    queryKey: qk.export(id),
    queryFn: () => getExportsById(id),
    enabled: id !== "",
    refetchInterval: (q) =>
      q.state.data?.state === "ready" || q.state.data?.state === "exporting"
        ? POLL
        : false,
  });
  const x = id ? progress.data : undefined;

  // To Trash, as deleting does; for good from Trash, or where there is none.
  const toTrash =
    folder.special_use !== "trash" &&
    boxes.some((mb) => mb.special_use === "trash" && mb.selectable);
  const remove = useMutation({
    mutationFn: () =>
      postExportsByIdDelete(id, { read: toTrash && readOn.trash }),
    onSuccess: ({ trash, deleted }) => {
      say(
        deleted === 0
          ? `Moved ${count(trash)} to Trash.`
          : trash === 0
            ? `Deleted ${count(deleted)} for good.`
            : `Moved ${count(trash)} to Trash and deleted ${count(deleted)} for good.`,
      );
      void client.invalidateQueries({ queryKey: qk.lists(config) });
      void client.invalidateQueries({ queryKey: qk.mailboxes(config) });
      onDeleted?.();
      onClose();
    },
    onError: (err) => say(messageOf(err)),
  });
  const askRemove = async (n: number) => {
    if (
      await confirm({
        title: `Delete the ${count(n)} exported?`,
        message: toTrash
          ? "They are moved to Trash. Mail that arrived since the export stays."
          : "They are deleted from the server for good. Mail that arrived since the export stays. This cannot be undone.",
        confirm: "Delete",
        danger: !toTrash,
      })
    ) {
      remove.mutate();
    }
  };

  const title = messages
    ? `Export ${count(messages.length)}`
    : `Export ${name}`;
  let body;
  let footer;
  if (!x) {
    body = (
      <form
        id="export"
        aria-label={title}
        className="flex flex-col gap-4"
        onSubmit={(e) => {
          e.preventDefault();
          start.mutate();
        }}
      >
        <Choice
          title="Format"
          note={
            format === "mbox"
              ? "One file every mail app imports. Read and flagged are kept."
              : one
                ? "The message as the server holds it. Read and flagged are not kept."
                : "A zip of one file per message, as the server holds it, named by date. Read and flagged are not kept."
          }
        >
          <Select
            size="bar"
            aria-label="Format"
            className="w-32 shrink-0"
            value={format}
            onChange={(e) => setFormat(e.target.value as "mbox" | "eml")}
          >
            <option value="mbox">mbox</option>
            <option value="eml">eml</option>
          </Select>
        </Choice>
        {!messages && inside > 0 ? (
          <Choice
            title="With its subfolders"
            note={`${inside === 1 ? "1 folder" : `${inside} folders`} inside it, in a zip as a tree.`}
          >
            <Switch
              on={subfolders}
              label="With its subfolders"
              onChange={setSubfolders}
            />
          </Choice>
        ) : null}
      </form>
    );
    footer = (
      <>
        {start.error ? (
          <p role="alert" className="w-full text-sm text-accent">
            {messageOf(start.error)}
          </p>
        ) : null}
        <Button onClick={onClose}>Cancel</Button>
        <Button
          type="submit"
          form="export"
          variant="solid"
          disabled={start.isPending}
        >
          Export
        </Button>
      </>
    );
  } else if (x.state === "failed") {
    body = (
      <p role="alert" className="text-sm text-accent">
        {x.error}
      </p>
    );
    footer = <Button onClick={onClose}>Close</Button>;
  } else if (x.state === "done") {
    body = (
      <div className="flex flex-col gap-2 text-sm">
        <p>
          Exported {count(x.exported)} as {x.file}.
        </p>
        {x.exported > 0 ? (
          <p className="text-muted">
            Once the file is saved, what went into it can be deleted here.
            emguio cannot see the file, so check it first.
          </p>
        ) : null}
      </div>
    );
    footer = (
      <>
        {x.exported > 0 ? (
          <Button
            variant="danger"
            disabled={remove.isPending}
            onClick={() => void askRemove(x.exported)}
          >
            Delete them…
          </Button>
        ) : null}
        <Button variant="solid" onClick={onClose}>
          Close
        </Button>
      </>
    );
  } else {
    const total = Math.max(x.total, x.exported);
    body = (
      <div className="flex flex-col gap-2 text-sm">
        <p>Exporting as {x.file}</p>
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
    footer = <Button onClick={onClose}>Close</Button>;
  }

  return (
    <Dialog open={open} onClose={onClose} title={title} footer={footer}>
      {body}
    </Dialog>
  );
}
