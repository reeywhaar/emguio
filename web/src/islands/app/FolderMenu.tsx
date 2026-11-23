import { useState } from "react";
import {
  useMutation,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query";

import {
  deleteEmailConfigsByIdMailboxesByMailbox,
  postEmailConfigsByIdMailboxesByMailboxDeleteOld,
  postEmailConfigsByIdMailboxesByMailboxRead,
} from "@app/api/actions/emailConfigs";
import { mk, qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { Mailbox, MessagePage } from "@app/api/types";
import { buttonLook } from "@app/components/Button";
import { useConfirm } from "@app/components/Confirm";
import { MoreMark } from "@app/components/icons";
import { Menu, MenuItem } from "@app/components/Menu";
import { useReadOn } from "@app/islands/app/Actions";
import { ExportDialog } from "@app/islands/app/ExportDialog";
import { FolderDialog } from "@app/islands/app/FolderDialog";
import { labelOfMailbox } from "@app/islands/app/mailbox";
import { say } from "@app/islands/app/notices";
import { go, paths } from "@app/islands/app/route";

/** How old mail is, in days, for Delete old mail to take it. */
const OLD = 30;

const messages = (n: number) => (n === 1 ? "1 message" : `${n} messages`);

/**
 * What can be done to a folder: all of it marked read, exported, its old mail deleted, and one of
 * the user's own renamed or moved, or deleted once empty. See docs/reading.md.
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
  const [exporting, setExporting] = useState({ open: false, opened: 0 });
  const own = folder.special_use === "";
  const name = labelOfMailbox(folder);
  const readOn = useReadOn(config);
  // Old mail goes to Trash, as deleting a message does; from Trash, or with none, for good.
  const toTrash = boxes.some(
    (mb) => mb.special_use === "trash" && mb.selectable && mb.id !== folder.id,
  );
  const deleteOld = useMutation({
    mutationKey: mk.folderWork(config),
    mutationFn: () =>
      postEmailConfigsByIdMailboxesByMailboxDeleteOld(config, folder.id, {
        days: OLD,
        read: toTrash && readOn.trash,
      }),
    onSuccess: ({ messages: n, trash }) =>
      say(
        n === 0
          ? `No mail in ${name} is older than ${OLD} days.`
          : trash
            ? `Moved ${messages(n)} to Trash.`
            : `Deleted ${messages(n)} for good.`,
      ),
    onError: (err) => say(messageOf(err)),
    onSettled: () => {
      void client.invalidateQueries({ queryKey: qk.lists(config) });
      void client.invalidateQueries({ queryKey: qk.mailboxes(config) });
    },
  });
  const askOld = async () => {
    if (
      await confirm({
        title: `Delete old mail from ${name}?`,
        message: toTrash
          ? `Mail that arrived in ${name} more than ${OLD} days ago is moved to Trash.`
          : `Mail that arrived in ${name} more than ${OLD} days ago is deleted from the server. This cannot be undone.`,
        confirm: "Delete old mail",
        danger: !toTrash,
      })
    ) {
      deleteOld.mutate();
    }
  };
  // Drawn read at once, and as it was if the server refuses.
  const read = useMutation({
    mutationKey: mk.folderWork(config),
    mutationFn: () =>
      postEmailConfigsByIdMailboxesByMailboxRead(config, folder.id),
    onMutate: async () => {
      await client.cancelQueries({ queryKey: qk.folder(config, folder.id) });
      const lists = client.getQueriesData<InfiniteData<MessagePage>>({
        queryKey: qk.folder(config, folder.id),
      });
      const counts = client.getQueryData<Mailbox[]>(qk.mailboxes(config));
      client.setQueriesData<InfiniteData<MessagePage>>(
        { queryKey: qk.folder(config, folder.id) },
        (old) =>
          old && {
            ...old,
            pages: old.pages.map((p) => ({
              ...p,
              messages: p.messages.map((m) => ({ ...m, seen: true })),
            })),
          },
      );
      client.setQueryData<Mailbox[]>(qk.mailboxes(config), (old) =>
        old?.map((mb) => (mb.id === folder.id ? { ...mb, unseen: 0 } : mb)),
      );
      return { lists, counts };
    },
    onError: (err, _v, before) => {
      for (const [key, data] of before?.lists ?? []) {
        client.setQueryData(key, data);
      }
      if (before?.counts) {
        client.setQueryData(qk.mailboxes(config), before.counts);
      }
      say(messageOf(err));
    },
    onSettled: () => {
      void client.invalidateQueries({ queryKey: qk.folder(config, folder.id) });
      void client.invalidateQueries({ queryKey: qk.mailboxes(config) });
    },
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
        title: `Delete ${name}?`,
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
            <MenuItem
              onClick={() => {
                close();
                setExporting((x) => ({ open: true, opened: x.opened + 1 }));
              }}
            >
              Export…
            </MenuItem>
            <MenuItem
              disabled={folder.messages === 0 || deleteOld.isPending}
              onClick={() => {
                close();
                void askOld();
              }}
            >
              Delete old mail…
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
      {exporting.opened > 0 ? (
        <ExportDialog
          key={exporting.opened}
          config={config}
          folder={folder}
          boxes={boxes}
          open={exporting.open}
          onClose={() => setExporting((x) => ({ ...x, open: false }))}
        />
      ) : null}
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
