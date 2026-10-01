import { useEffect, useLayoutEffect, useRef, type ReactNode } from "react";
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
import { mk, qk } from "@app/api/keys";
import { messageOf } from "@app/api/transport";
import type {
  Mailbox,
  Message,
  MessagePage,
  ReadMessage,
} from "@app/api/types";
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
import { say } from "@app/islands/app/notices";
import {
  counted,
  effects,
  listWithPending,
  usePending,
  type Change,
  type Pending,
} from "@app/islands/app/pending";
import { go, paths } from "@app/islands/app/route";

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
 * the server, drawn before it answers; see docs/reading.md.
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
  /** The message as far as it is known: the list's row until the whole of it has arrived. */
  m: Message | undefined;
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
  const reading = qk.message(config, mailbox.id, message);
  const sidebar = qk.mailboxes(config);
  const pending = usePending(config);

  /*
   * What the server confirmed, written into what it last said: the pending layer stops drawing
   * an action once it is answered, and this is what is drawn in its place. A read of the same
   * data still in flight began before the server acted, so it is cancelled, the confirmation
   * written, and the data read again.
   */
  const confirmed = async (
    keys: readonly (readonly unknown[])[],
    write: () => void,
  ) => {
    const stale = keys.filter(
      (queryKey) => client.isFetching({ queryKey }) > 0,
    );
    await Promise.all(
      keys.map((queryKey) => client.cancelQueries({ queryKey })),
    );
    write();
    for (const queryKey of stale) client.invalidateQueries({ queryKey });
  };
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
  const recount = (done: Pending) =>
    client.setQueryData<Mailbox[]>(
      sidebar,
      (old) => old && counted(old, effects(done)),
    );

  // Every action is drawn on the press by the pending layer and sent after, one at a time per
  // config and in the order pressed; see docs/reading.md.
  const queue = {
    mutationKey: mk.actionsOf(config),
    scope: { id: `action:${config}` },
  };

  const flags = useMutation({
    ...queue,
    mutationFn: (p: Pending & { kind: "flags" }) =>
      patchEmailConfigsByIdMailboxesByMailboxMessagesByMessage(
        config,
        p.mailbox,
        p.message,
        p.change,
      ),
    onSuccess: (updated, p) =>
      confirmed([list, reading, sidebar], () => {
        client.setQueryData<ReadMessage>(
          qk.message(config, p.mailbox, p.message),
          (old) => old && { ...old, ...updated },
        );
        rows((msgs) =>
          msgs.map((x) => (x.id === p.message ? { ...x, ...updated } : x)),
        );
        recount(p);
      }),
  });

  const move = useMutation({
    ...queue,
    mutationFn: (p: Pending & { kind: "move" }) =>
      p.to
        ? postEmailConfigsByIdMailboxesByMailboxMessagesByMessageMove(
            config,
            p.mailbox,
            p.message,
            p.to,
          )
        : deleteEmailConfigsByIdMailboxesByMailboxMessagesByMessage(
            config,
            p.mailbox,
            p.message,
          ),
    onSuccess: (_, p) =>
      confirmed([list, sidebar], () => {
        rows((msgs) => msgs.filter((x) => x.id !== p.message));
        recount(p);
        if (p.to)
          client.invalidateQueries({ queryKey: qk.messages(config, p.to) });
      }),
    onError: (err, p) => {
      const what = m?.subject ? `“${m.subject}”` : "The message";
      say(`${what} was not ${p.to ? "moved" : "deleted"}: ${messageOf(err)}`);
    },
  });

  const setFlags = (change: Change) =>
    flags.mutate({
      kind: "flags",
      mailbox: mailbox.id,
      message,
      change,
      seen: m?.seen ?? true,
    });

  // On the press: the pending layer takes the message out of the list, and the pane goes on to
  // the one below it in the list as drawn, or else the one above, or else the folder.
  const moveTo = (dest: Mailbox | null) => {
    const drawn = listWithPending(
      client
        .getQueryData<InfiniteData<MessagePage>>(list)
        ?.pages.flatMap((p) => p.messages) ?? [],
      mailbox.id,
      pending,
    );
    const at = drawn.findIndex((x) => x.id === message);
    const neighbor = at >= 0 ? (drawn[at + 1] ?? drawn[at - 1]) : undefined;
    move.mutate({
      kind: "move",
      mailbox: mailbox.id,
      message,
      to: dest?.id ?? null,
      seen: m?.seen ?? true,
    });
    go(paths.mail(config, mailbox.id, neighbor?.id));
  };

  // Opening an unread message is reading it, once per opening: see docs/reading.md. Before the
  // first paint, so the pane never shows it unread for a moment and then not.
  const { mutate } = flags;
  const marked = useRef(false);
  const unread = m !== undefined && !m.seen;
  useLayoutEffect(() => {
    if (!unread || marked.current) return;
    marked.current = true;
    mutate({
      kind: "flags",
      mailbox: mailbox.id,
      message,
      change: { seen: true },
      seen: false,
    });
  }, [unread, mutate, mailbox.id, message]);

  const seen = m?.seen ?? true;
  const starred = m?.flagged ?? false;
  // Only a message nothing is known of yet — opened from a link before its folder's list — has
  // no flags to act on.
  const busy = !m;

  const remove = async () => {
    if (to.trash) return moveTo(to.trash);
    const yes = await confirm({
      title: "Delete this message for good?",
      message:
        to.trash === undefined && mailbox.special_use !== "trash"
          ? "This server has no Trash folder, so it cannot be brought back."
          : "It is removed from the server and cannot be brought back.",
      confirm: "Delete forever",
      danger: true,
    });
    if (yes) moveTo(null);
  };
  const forever = !to.trash;

  const act = {
    archive: to.archive ? () => moveTo(to.archive!) : undefined,
    remove,
    spam: to.spam ? () => moveTo(to.spam!) : undefined,
    notSpam: to.notSpam ? () => moveTo(to.notSpam!) : undefined,
    star: () => setFlags({ flagged: !starred }),
    seen: () => setFlags({ seen: !seen }),
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
  // A move's refusal is said by the notice, because the pane has already moved on.
  const error = flags.error;

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
                if (dest) moveTo(dest);
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
