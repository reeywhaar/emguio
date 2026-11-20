import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import {
  deleteEmailConfigsByIdMailboxesByMailbox,
  postEmailConfigsByIdMailboxesByMailboxRead,
} from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { Mailbox } from "@app/api/types";
import { buttonLook } from "@app/components/Button";
import { useConfirm } from "@app/components/Confirm";
import { MoreMark } from "@app/components/icons";
import { Menu, MenuItem } from "@app/components/Menu";
import { FolderDialog } from "@app/islands/app/FolderDialog";
import { labelOfMailbox } from "@app/islands/app/mailbox";
import { say } from "@app/islands/app/notices";
import { go, paths } from "@app/islands/app/route";

/**
 * What can be done to a folder: all of it marked read, and one of the user's own renamed or
 * moved, or deleted once empty. See docs/reading.md.
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
  const own = folder.special_use === "";
  const read = useMutation({
    mutationFn: () =>
      postEmailConfigsByIdMailboxesByMailboxRead(config, folder.id),
    onSettled: () => {
      void client.invalidateQueries({ queryKey: qk.folder(config, folder.id) });
      void client.invalidateQueries({ queryKey: qk.mailboxes(config) });
    },
    onError: (err) => say(messageOf(err)),
  });
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
  const drop = async () => {
    if (
      await confirm({
        title: `Delete ${labelOfMailbox(folder)}?`,
        message:
          "It is deleted from the server. A folder with mail or other folders in it is not.",
        confirm: "Delete",
        danger: true,
      })
    ) {
      remove.mutate();
    }
  };

  return (
    <>
      <Menu
        name="Folder"
        title="Folder"
        chevron={false}
        end
        look={buttonLook("quiet", "bar")}
        disabled={remove.isPending}
        label={<MoreMark />}
      >
        {(close) => (
          <>
            <MenuItem
              disabled={folder.unseen === 0 || read.isPending}
              onClick={() => {
                close();
                read.mutate();
              }}
            >
              Mark all as read
            </MenuItem>
            {own ? (
              <>
                <MenuItem
                  onClick={() => {
                    close();
                    setOpened((n) => n + 1);
                    setEditing(true);
                  }}
                >
                  Rename or move…
                </MenuItem>
                <MenuItem
                  danger
                  onClick={() => {
                    close();
                    void drop();
                  }}
                >
                  Delete…
                </MenuItem>
              </>
            ) : null}
          </>
        )}
      </Menu>
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
