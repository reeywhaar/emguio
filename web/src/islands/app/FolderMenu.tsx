import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { deleteEmailConfigsByIdMailboxesByMailbox } from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { Mailbox } from "@app/api/types";
import { buttonLook } from "@app/components/Button";
import { useConfirm } from "@app/components/Confirm";
import { MoreMark } from "@app/components/icons";
import { FolderDialog } from "@app/islands/app/FolderDialog";
import { labelOfMailbox } from "@app/islands/app/mailbox";
import { say } from "@app/islands/app/notices";
import { go, paths } from "@app/islands/app/route";

/**
 * What can be done to a folder of the user's own: renamed or moved, or deleted once empty. The
 * platform's own list over a button, as the folder picker is. See docs/reading.md.
 */
export function FolderMenu({
  config,
  boxes,
  folder,
}: {
  config: string;
  boxes: Mailbox[];
  folder: Mailbox;
}) {
  const client = useQueryClient();
  const confirm = useConfirm();
  const [editing, setEditing] = useState(false);
  // Each opening starts the form afresh.
  const [opened, setOpened] = useState(0);
  const remove = useMutation({
    mutationFn: () =>
      deleteEmailConfigsByIdMailboxesByMailbox(config, folder.id),
    onSuccess: () => {
      client.setQueryData<Mailbox[]>(qk.mailboxes(config), (old) =>
        old?.filter((mb) => mb.id !== folder.id),
      );
      void client.invalidateQueries({ queryKey: qk.mailboxes(config) });
      go(paths.mail(config));
    },
    onError: (err) => say(messageOf(err)),
  });

  const choose = async (what: string) => {
    if (what === "edit") {
      setOpened((n) => n + 1);
      setEditing(true);
      return;
    }
    if (
      what === "delete" &&
      (await confirm({
        title: `Delete ${labelOfMailbox(folder)}?`,
        message:
          "It is deleted from the server. A folder with mail or other folders in it is not.",
        confirm: "Delete",
        danger: true,
      }))
    ) {
      remove.mutate();
    }
  };

  return (
    <>
      <label
        title="Folder"
        className={`${buttonLook("quiet", "bar")} relative focus-within:border-brand`}
      >
        <MoreMark />
        <select
          aria-label="Folder"
          className="absolute inset-0 cursor-pointer opacity-0 disabled:cursor-default"
          value=""
          disabled={remove.isPending}
          onChange={(e) => void choose(e.target.value)}
        >
          <option value="">{labelOfMailbox(folder)}</option>
          <option value="edit">Rename or move…</option>
          <option value="delete">Delete…</option>
        </select>
      </label>
      {/* Beside the picker rather than in it: a click inside the dialog would be the label's. */}
      <FolderDialog
        key={opened}
        config={config}
        boxes={boxes}
        folder={folder}
        open={editing}
        onClose={() => setEditing(false)}
        onDone={() => setEditing(false)}
      />
    </>
  );
}
