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
import { ApiError, messageOf } from "@app/api/transport";
import type { Mailbox, MessagePage } from "@app/api/types";
import { buttonLook } from "@app/components/Button";
import { useConfirm } from "@app/components/Confirm";
import { MoreMark } from "@app/components/icons";
import { Menu, MenuItem, MenuSeparator } from "@app/components/Menu";
import { useReadOn } from "@app/islands/app/Actions";
import { useExportMail } from "@app/islands/app/Exports";
import { ImportDialog } from "@app/islands/app/ImportDialog";
import { FolderDialog } from "@app/islands/app/FolderDialog";
import { labelOfMailbox, within } from "@app/islands/app/mailbox";
import { say } from "@app/islands/app/notices";
import { go, paths } from "@app/islands/app/route";

/** How old mail is, in days, for Delete old mail to take it. */
const OLD = 30;

const messages = (n: number) => (n === 1 ? "1 message" : `${n} messages`);

/**
 * What can be done to a folder: all of it marked read, exported or mail imported into it, its old
 * mail deleted, and one of the user's own renamed or moved, or deleted once empty. See
 * docs/reading.md.
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
  const exportMail = useExportMail(config);
  // A folder goes with its subfolders.
  const nested = boxes.some((mb) => within(mb, folder));
  const [importing, setImporting] = useState({ open: false, opened: 0 });
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
  // What goes with it: the folders inside it and the mail in all of them, as last counted.
  const inside = boxes.filter((mb) => within(mb, folder));
  const held = [folder, ...inside].reduce((n, mb) => n + mb.messages, 0);
  // The mail goes to Trash, as deleting does; from inside Trash, or with none, for good.
  const trash = boxes.find((mb) => mb.special_use === "trash" && mb.selectable);
  const mailToTrash =
    trash !== undefined && trash.id !== folder.id && !within(folder, trash);
  const remove = useMutation({
    mutationFn: (withMail: boolean) =>
      deleteEmailConfigsByIdMailboxesByMailbox(
        config,
        folder.id,
        withMail ? { read: mailToTrash && readOn.trash } : undefined,
      ),
    onSuccess: () => {
      client.setQueryData<Mailbox[]>(qk.mailboxes(config), (old) =>
        old?.filter((mb) => mb.id !== folder.id && !within(mb, folder)),
      );
      void client.invalidateQueries({ queryKey: qk.mailboxes(config) });
      void client.invalidateQueries({ queryKey: qk.lists(config) });
      go(paths.mail(config));
    },
    onError: async (err) => {
      // It held more than was counted: asked again, with what the server says it holds.
      if (err instanceof ApiError && err.code === "not_empty") {
        if (
          await confirm({
            title: `Delete the mail in ${name} too?`,
            message: `${err.message} ${mailToTrash ? "The mail goes to Trash, then the folder is deleted." : "The mail is deleted for good, then the folder. This cannot be undone."}`,
            confirm: "Delete",
            danger: true,
          })
        ) {
          remove.mutate(true);
        }
        return;
      }
      say(messageOf(err));
    },
  });
  const drop = async () => {
    const what = [
      inside.length === 1
        ? "the folder inside it"
        : inside.length > 1
          ? `the ${inside.length} folders inside it`
          : "",
      held > 0
        ? `the ${messages(held)} in ${inside.length > 0 ? "them" : "it"}`
        : "",
    ].filter(Boolean);
    const withMail = what.length > 0;
    if (
      await confirm({
        title: withMail
          ? `Delete ${name}${what.length === 2 ? ", " : " and "}${what.join(" and ")}?`
          : `Delete ${name}?`,
        message: !withMail
          ? "It is deleted from the server."
          : held === 0
            ? "They are deleted from the server, the deepest first."
            : mailToTrash
              ? "The mail goes to Trash, then the folders are deleted from the server."
              : "The mail is deleted from the server for good, then the folders. This cannot be undone.",
        confirm: "Delete",
        danger: true,
      })
    ) {
      remove.mutate(withMail);
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
            <MenuSeparator />
            {(["mbox", "eml"] as const).map((format) => (
              <MenuItem
                key={format}
                disabled={!folder.selectable && !nested}
                onClick={() => {
                  close();
                  exportMail.mutate({
                    mailbox: folder.id,
                    format,
                    messages: [],
                    subfolders: nested,
                  });
                }}
              >
                Export as {format}
              </MenuItem>
            ))}
            <MenuItem
              disabled={!folder.selectable}
              onClick={() => {
                close();
                setImporting((x) => ({ open: true, opened: x.opened + 1 }));
              }}
            >
              Import mail…
            </MenuItem>
          </>
        )}
      </Menu>
      {importing.opened > 0 ? (
        <ImportDialog
          key={importing.opened}
          config={config}
          folder={folder}
          open={importing.open}
          onClose={() => setImporting((x) => ({ ...x, open: false }))}
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
