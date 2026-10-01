import { useEffect, useRef, type ReactNode } from "react";
import {
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query";

import {
  deleteEmailConfigsByIdMailboxesByMailboxMessagesByMessage,
  getEmailConfigsByIdMailboxes,
  patchEmailConfigsByIdMailboxesByMailboxMessagesByMessage,
  postEmailConfigsByIdMailboxesByMailboxMessagesByMessageMove,
} from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type { Mailbox, MessagePage, ReadMessage } from "@app/api/types";
import { Button, buttonLook } from "@app/components/Button";
import { useConfirm } from "@app/components/Confirm";
import {
  ArchiveMark,
  FolderMark,
  InboxMark,
  MailMark,
  SpamMark,
  StarMark,
  TrashMark,
} from "@app/components/icons";
import { depthOf, labelOfMailbox } from "@app/islands/app/mailbox";
import { go, paths } from "@app/islands/app/route";

type Change = { seen?: boolean; flagged?: boolean };

/**
 * Where each action sends a message: the folders the server says are for archiving, for
 * deleted mail and for spam. Archive is Gmail's All Mail where there is no Archive. An action
 * whose folder is the one the message is in, or that the server does not have, is not offered.
 */
export function targets(boxes: Mailbox[], current: Mailbox) {
  const find = (...uses: Mailbox["special_use"][]) =>
    uses
      .map((use) => boxes.find((mb) => mb.special_use === use && mb.selectable))
      .find((mb) => mb !== undefined);
  const archive = find("archive", "all");
  const trash = find("trash");
  const junk = find("junk");
  const inbox = find("inbox");
  const here = (mb: Mailbox | undefined) => mb?.id === current.id;
  return {
    archive: here(archive) ? undefined : archive,
    // In Trash, or where there is none, deleting is for good.
    trash: here(trash) ? undefined : trash,
    spam: here(junk) ? undefined : junk,
    notSpam: here(junk) && !here(inbox) ? inbox : undefined,
  };
}

/**
 * What can be done to the open message, with the way back to its folder at the start of the
 * row: read or unread, starred, archived, deleted, spam, or moved anywhere. Each is a request to
 * the server, and the list and the sidebar follow once it has answered.
 */
export function Actions({
  config,
  mailbox,
  message,
  m,
  back,
}: {
  config: string;
  mailbox: Mailbox;
  message: string;
  m: ReadMessage | undefined;
  back: ReactNode;
}) {
  const client = useQueryClient();
  const confirm = useConfirm();
  const boxes = useQuery({
    queryKey: qk.mailboxes(config),
    queryFn: () => getEmailConfigsByIdMailboxes(config),
  });
  const to = targets(boxes.data ?? [], mailbox);

  // The list it is in is this folder's, and only that one: an id names a message within its
  // folder, so another folder's list can hold the same one for a different message.
  const list = qk.messages(config, mailbox.id);
  const rows = (
    edit: (msgs: MessagePage["messages"]) => MessagePage["messages"],
  ) =>
    client.setQueryData<InfiniteData<MessagePage>>(
      list,
      (old) =>
        old && {
          ...old,
          pages: old.pages.map((p) => ({ ...p, messages: edit(p.messages) })),
        },
    );

  const flags = useMutation({
    mutationFn: (change: Change) =>
      patchEmailConfigsByIdMailboxesByMailboxMessagesByMessage(
        config,
        mailbox.id,
        message,
        change,
      ),
    onSuccess: (updated) => {
      client.setQueryData<ReadMessage>(
        qk.message(config, mailbox.id, message),
        (old) => old && { ...old, ...updated },
      );
      rows((msgs) =>
        msgs.map((x) => (x.id === message ? { ...x, ...updated } : x)),
      );
      client.invalidateQueries({ queryKey: qk.mailboxes(config) });
    },
  });

  const move = useMutation({
    mutationFn: (dest: Mailbox | null) =>
      dest
        ? postEmailConfigsByIdMailboxesByMailboxMessagesByMessageMove(
            config,
            mailbox.id,
            message,
            dest.id,
          )
        : deleteEmailConfigsByIdMailboxesByMailboxMessagesByMessage(
            config,
            mailbox.id,
            message,
          ),
    onSuccess: (_, dest) => {
      rows((msgs) => msgs.filter((x) => x.id !== message));
      client.invalidateQueries({ queryKey: qk.mailboxes(config) });
      if (dest)
        client.invalidateQueries({ queryKey: qk.messages(config, dest.id) });
      go(paths.mail(config, mailbox.id));
    },
  });

  // Opening an unread message is reading it, once per opening: see docs/reading.md.
  const { mutate } = flags;
  const marked = useRef(false);
  const unread = m !== undefined && !m.seen;
  useEffect(() => {
    if (!unread || marked.current) return;
    marked.current = true;
    mutate({ seen: true });
  }, [unread, mutate]);

  // What each flag is about to be while the server is asked, so a button does not flicker.
  const pending = flags.isPending ? flags.variables : undefined;
  const seen = pending?.seen ?? m?.seen ?? true;
  const starred = pending?.flagged ?? m?.flagged ?? false;
  // An unread message about to be marked read counts as busy too, so the buttons do not light
  // for the moment between it arriving and the marking starting.
  const settling = unread && flags.isIdle;
  const busy = !m || settling || flags.isPending || move.isPending;

  const remove = async () => {
    if (to.trash) return move.mutate(to.trash);
    const yes = await confirm({
      title: "Delete this message for good?",
      message:
        to.trash === undefined && mailbox.special_use !== "trash"
          ? "This server has no Trash folder, so it cannot be brought back."
          : "It is removed from the server and cannot be brought back.",
      confirm: "Delete forever",
      danger: true,
    });
    if (yes) move.mutate(null);
  };
  const forever = !to.trash;

  const act = {
    archive: to.archive ? () => move.mutate(to.archive!) : undefined,
    remove,
    spam: to.spam ? () => move.mutate(to.spam!) : undefined,
    notSpam: to.notSpam ? () => move.mutate(to.notSpam!) : undefined,
    star: () => flags.mutate({ flagged: !starred }),
    seen: () => flags.mutate({ seen: !seen }),
  };

  // Gmail's keys, which hands that know Gmail already reach for. Not while a field has the keys,
  // nor while a dialog is open; the message itself, in its frame, keeps them to itself.
  const latest = useRef({ act, busy });
  latest.current = { act, busy };
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const now = latest.current;
      if (now.busy || e.metaKey || e.ctrlKey || e.altKey) return;
      const el = e.target instanceof HTMLElement ? e.target : null;
      if (el?.closest("input, textarea, select, [contenteditable='true']"))
        return;
      if (document.querySelector("dialog[open]")) return;
      const run = {
        e: now.act.archive,
        "#": now.act.remove,
        "!": now.act.spam ?? now.act.notSpam,
        s: now.act.star,
        u: now.act.seen,
      }[e.key];
      if (!run) return;
      e.preventDefault();
      run();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const others = (boxes.data ?? []).filter(
    (mb) => mb.selectable && mb.id !== mailbox.id,
  );
  const error = flags.error ?? move.error;

  return (
    <>
      <div className="flex flex-wrap items-center gap-1">
        {back}
        <span className="flex-1" />
        {act.archive ? (
          <Action
            label="Archive"
            keyName="e"
            disabled={busy}
            onClick={act.archive}
          >
            <ArchiveMark />
          </Action>
        ) : null}
        <Action
          label={forever ? "Delete forever…" : "Delete"}
          keyName="#"
          disabled={busy}
          onClick={act.remove}
        >
          <TrashMark />
        </Action>
        {act.spam ? (
          <Action label="Spam" keyName="!" disabled={busy} onClick={act.spam}>
            <SpamMark />
          </Action>
        ) : null}
        {act.notSpam ? (
          <Action
            label="Not spam"
            keyName="!"
            disabled={busy}
            onClick={act.notSpam}
          >
            <InboxMark />
          </Action>
        ) : null}
        <Action
          label="Star"
          keyName="s"
          disabled={busy}
          onClick={act.star}
          pressed={starred}
        >
          <StarMark filled={starred} />
        </Action>
        <Action
          label={seen ? "Mark as unread" : "Mark as read"}
          keyName="u"
          disabled={busy}
          onClick={act.seen}
        >
          <MailMark open={seen} />
        </Action>
        {others.length > 0 ? (
          // The platform's own list of folders, laid over a button that looks like the others,
          // so on a phone it is the system's picker and in the row it is one more icon.
          <label
            title="Move to folder"
            className={`${buttonLook("quiet", "bar")} relative focus-within:border-brand ${busy ? "opacity-50" : ""}`}
          >
            <FolderMark />
            <select
              aria-label="Move to folder"
              className="absolute inset-0 cursor-pointer opacity-0 disabled:cursor-default"
              value=""
              disabled={busy}
              onChange={(e) => {
                const dest = others.find((mb) => mb.id === e.target.value);
                if (dest) move.mutate(dest);
              }}
            >
              <option value="">Move to…</option>
              {others.map((mb) => (
                <option key={mb.id} value={mb.id}>
                  {" ".repeat(depthOf(mb))}
                  {labelOfMailbox(mb)}
                </option>
              ))}
            </select>
          </label>
        ) : null}
      </div>
      {error ? (
        <p role="alert" className="text-sm text-accent">
          {messageOf(error)}
        </p>
      ) : null}
    </>
  );
}

/** A button that is an icon, named for what it does, with its key in the tooltip. */
function Action({
  label,
  keyName,
  pressed,
  children,
  ...props
}: {
  label: string;
  keyName: string;
  pressed?: boolean;
  children: ReactNode;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <Button
      size="bar"
      aria-label={label}
      aria-pressed={pressed}
      title={`${label} (${keyName})`}
      {...props}
    >
      {children}
    </Button>
  );
}
