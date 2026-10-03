import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  useQuery,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query";

import {
  getEmailConfigs,
  getEmailConfigsByIdMailboxes,
} from "@app/api/actions/emailConfigs";
import { qk } from "@app/api/keys";
import type { JobDraft, Mailbox, Message, MessagePage } from "@app/api/types";
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
import { useAsk } from "@app/islands/app/ask";
import { depthOf, labelOfMailbox } from "@app/islands/app/mailbox";
import { NewFolder } from "@app/islands/app/NewFolder";
import { listWithPending, usePending } from "@app/islands/app/pending";
import { go, paths } from "@app/islands/app/route";

/**
 * Where each action sends a message: the folders the server says are for archiving, for
 * deleted mail and for spam. Archive is the folder the user chose, or else the server's Archive,
 * or Gmail's All Mail where there is none. An action whose folder is the one the message is in,
 * or that the server does not have, is not offered.
 */
export function targets(
  boxes: Mailbox[],
  current: Mailbox,
  archiveTo: string | null = null,
) {
  const find = (...uses: Mailbox["special_use"][]) =>
    uses
      .map((use) => boxes.find((mb) => mb.special_use === use && mb.selectable))
      .find((mb) => mb !== undefined);
  const archive =
    boxes.find((mb) => mb.id === archiveTo && mb.selectable) ??
    find("archive", "all");
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

/** The folder a config archives to, as its user chose; null for the one the server names. */
export function useArchiveTo(config: string): string | null {
  const configs = useQuery({
    queryKey: qk.emailConfigs,
    queryFn: getEmailConfigs,
  });
  return configs.data?.find((c) => c.id === config)?.archive_mailbox ?? null;
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
  q = "",
  m,
  back,
}: {
  config: string;
  mailbox: Mailbox;
  message: string;
  /** What the folder was searched for, if the message was opened from what it found. */
  q?: string;
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
  const to = targets(boxes.data ?? [], mailbox, useArchiveTo(config));

  // The list it is in is this folder's, or what it was searched for, and only that one: an id
  // names a message within its folder, so another folder's list can hold the same one for a
  // different message.
  const list = qk.messages(config, mailbox.id, q);
  const pending = usePending(config);

  const asked = useAsk(config);
  const ask = (job: Pick<JobDraft, "kind"> & Partial<JobDraft>) =>
    asked([
      {
        email_config: config,
        mailbox: mailbox.id,
        message,
        value: false,
        target: "",
        seen: m?.seen ?? true,
        label: m?.subject,
        ...job,
      },
    ]);

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
    ask(dest ? { kind: "move", target: dest.id } : { kind: "delete" });
    go(paths.mail(config, mailbox.id, neighbor?.id, q));
  };

  // Opening an unread message is reading it, once per opening: see docs/reading.md. Before the
  // first paint, so the pane never shows it unread for a moment and then not.
  // Decided once, by how it opened: marked unread while open, it stays so.
  const opened = useRef(ask);
  opened.current = ask;
  const decided = useRef(false);
  const known = m !== undefined;
  const unread = known && !m.seen;
  useLayoutEffect(() => {
    if (!known || decided.current) return;
    decided.current = true;
    if (unread) opened.current({ kind: "seen", value: true, seen: false });
  }, [known, unread]);

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
    star: () => ask({ kind: "flagged", value: !starred }),
    seen: () => ask({ kind: "seen", value: !seen }),
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
      if (el?.closest("input, textarea, select, [contenteditable='true']")) {
        return;
      }
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
        <MovePicker
          config={config}
          boxes={boxes.data ?? []}
          current={mailbox}
          disabled={busy}
          onMove={moveTo}
        />
      </div>
    </>
  );
}

/** A button that is an icon, named for what it does, with its key in the tooltip. */
export function Action({
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

/** The picker's choice that makes a folder to move to. */
const NEW = "new";

/**
 * Every other folder a message can be moved to, or a new one: the platform's own list, laid over
 * a button that looks like the others, so on a phone it is the system's picker and in a row of
 * actions it is one more icon.
 */
export function MovePicker({
  config,
  boxes,
  current,
  disabled,
  onMove,
}: {
  config: string;
  boxes: Mailbox[];
  current: Mailbox;
  disabled: boolean;
  onMove: (to: Mailbox) => void;
}) {
  const [making, setMaking] = useState(false);
  // Each opening starts the form afresh.
  const [opened, setOpened] = useState(0);
  const others = boxes.filter((mb) => mb.selectable && mb.id !== current.id);
  return (
    <>
      <label
        title="Move to folder"
        className={`${buttonLook("quiet", "bar")} relative focus-within:border-brand ${disabled ? "opacity-50" : ""}`}
      >
        <FolderMark />
        <select
          aria-label="Move to folder"
          className="absolute inset-0 cursor-pointer opacity-0 disabled:cursor-default"
          value=""
          disabled={disabled}
          onChange={(e) => {
            if (e.target.value === NEW) {
              setOpened((n) => n + 1);
              setMaking(true);
              return;
            }
            const dest = others.find((mb) => mb.id === e.target.value);
            if (dest) onMove(dest);
          }}
        >
          <option value="">Move to…</option>
          {others.map((mb) => (
            <option key={mb.id} value={mb.id}>
              {" ".repeat(depthOf(mb))}
              {labelOfMailbox(mb)}
            </option>
          ))}
          <option value={NEW}>New folder…</option>
        </select>
      </label>
      {/* Beside the picker rather than in it: a click inside the dialog would be the label's. */}
      <NewFolder
        key={opened}
        config={config}
        boxes={boxes}
        open={making}
        onClose={() => setMaking(false)}
        onMade={(made) => {
          setMaking(false);
          onMove(made);
        }}
      />
    </>
  );
}
