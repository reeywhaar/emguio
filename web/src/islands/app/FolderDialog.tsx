import { useId, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import {
  postEmailConfigsByIdMailboxes,
  putEmailConfigsByIdMailboxesByMailbox,
} from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { Mailbox } from "@app/api/types";
import { Button } from "@app/components/Button";
import { Dialog } from "@app/components/Dialog";
import { Field, Select } from "@app/components/Field";
import { TextField } from "@app/components/TextField";
import { depthOf, labelOfMailbox } from "@app/islands/app/mailbox";

/** Whether a's path starts with all of b's, and is longer: a is somewhere inside b. */
const within = (a: Mailbox, b: Mailbox) =>
  a.path.length > b.path.length && b.path.every((p, i) => a.path[i] === p);

/**
 * A folder on the mail server, named and placed — at the top or inside another: made on its own,
 * made for a move to go to, or, given folder, renamed or moved.
 */
export function FolderDialog({
  config,
  boxes,
  folder,
  moving = false,
  open,
  onClose,
  onDone,
}: {
  config: string;
  boxes: Mailbox[];
  /** The folder renamed or moved; none makes one. */
  folder?: Mailbox;
  /** Made for what is being moved, which goes to it next. */
  moving?: boolean;
  open: boolean;
  onClose: () => void;
  onDone: (folder: Mailbox) => void;
}) {
  const client = useQueryClient();
  const form = useId();
  const [name, setName] = useState(folder?.path.at(-1) ?? "");
  const [parent, setParent] = useState(
    () =>
      (folder &&
        boxes.find(
          (mb) =>
            mb.path.length === folder.path.length - 1 && within(folder, mb),
        )?.id) ??
      "",
  );
  const save = useMutation({
    mutationFn: () => {
      const body = { name: name.trim(), parent };
      return folder
        ? putEmailConfigsByIdMailboxesByMailbox(config, folder.id, body)
        : postEmailConfigsByIdMailboxes(config, body);
    },
    onSuccess: (done) => {
      // There at once; the sidebar's order, and what moved with it, follow from the next read.
      client.setQueryData<Mailbox[]>(
        qk.mailboxes(config),
        (old) => old && [...old.filter((mb) => mb.id !== done.id), done],
      );
      void client.invalidateQueries({ queryKey: qk.mailboxes(config) });
      onDone(done);
    },
  });
  // Nothing goes inside itself.
  const places = folder
    ? boxes.filter((mb) => mb.id !== folder.id && !within(mb, folder))
    : boxes;

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title={
        folder
          ? "Rename or move folder"
          : moving
            ? "Move to a new folder"
            : "New folder"
      }
      footer={
        <>
          {save.error ? (
            <p role="alert" className="w-full text-sm text-accent">
              {messageOf(save.error)}
            </p>
          ) : null}
          <Button onClick={onClose}>Cancel</Button>
          <Button
            type="submit"
            form={form}
            variant="solid"
            disabled={save.isPending || name.trim() === ""}
          >
            {save.isPending
              ? "Saving"
              : folder
                ? "Save"
                : moving
                  ? "Make and move"
                  : "Make"}
          </Button>
        </>
      }
    >
      <form
        id={form}
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <Field label="Name">
          <TextField
            value={name}
            onChange={(e) => setName(e.target.value)}
            data-autofocus
          />
        </Field>
        <Field label="Inside">
          <Select value={parent} onChange={(e) => setParent(e.target.value)}>
            <option value="">No folder, at the top</option>
            {places.map((mb) => (
              <option key={mb.id} value={mb.id}>
                {" ".repeat(depthOf(mb))}
                {labelOfMailbox(mb)}
              </option>
            ))}
          </Select>
        </Field>
      </form>
    </Dialog>
  );
}
