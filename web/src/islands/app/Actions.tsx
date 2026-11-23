import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  useQueries,
  useQuery,
  useQueryClient,
  type InfiniteData,
} from "@tanstack/react-query";

import {
  getEmailConfigs,
  getEmailConfigsByIdMailboxes,
} from "@app/api/actions/emailConfigs";
import { getReading } from "@app/api/actions/reading";
import { qk } from "@app/api/keys";
import { labelOf, turnedOn } from "@app/islands/app/emailConfig";
import type {
  EmailConfig,
  JobDraft,
  Mailbox,
  Message,
  MessagePage,
} from "@app/api/types";
import { Button, buttonLook } from "@app/components/Button";
import { useConfirm } from "@app/components/Confirm";
import { Dialog } from "@app/components/Dialog";
import { Dummy } from "@app/components/Dummy";
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
import { FolderDialog } from "@app/islands/app/FolderDialog";
import {
  listWithPending,
  usePending,
  type Pending,
} from "@app/islands/app/pending";
import { go, paths } from "@app/islands/app/route";
import { SIDE_BY_SIDE } from "@app/media";

/**
 * Whether a config's mail is marked read before it is archived, and before it is deleted: its own
 * choice, or else the user's. See docs/reading.md.
 */
export function useReadOn(config: string): {
  archive: boolean;
  trash: boolean;
} {
  const configs = useQuery({
    queryKey: qk.emailConfigs,
    queryFn: getEmailConfigs,
  });
  const reading = useQuery({ queryKey: qk.reading, queryFn: getReading });
  const c = configs.data?.find((each) => each.id === config);
  return {
    archive: c?.read_on_archive ?? reading.data?.read_on_archive ?? false,
    trash: c?.read_on_trash ?? reading.data?.read_on_trash ?? false,
  };
}

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
  const chosen = boxes.find((mb) => mb.id === archiveTo && mb.selectable);
  const archive =
    chosen && ["", "archive", "all"].includes(chosen.special_use)
      ? chosen
      : find("archive", "all");
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
  const readOn = useReadOn(config);
  const draft = (job: Pick<JobDraft, "kind"> & Partial<JobDraft>) => ({
    email_config: config,
    mailbox: mailbox.id,
    message,
    value: false,
    target: "",
    seen: m?.seen ?? true,
    label: m?.subject,
    ...job,
  });
  const ask = (job: Pick<JobDraft, "kind"> & Partial<JobDraft>) =>
    asked([draft(job)]);

  // On the press: the pending layer takes the message out of the list. Beside the list, the pane
  // goes on to the one below it as drawn, or else the one above, or else the folder; in the
  // list's place, back to the list.
  const moveTo = (dest: Mailbox | null, elsewhere = false, read = false) => {
    const beside = window.matchMedia(SIDE_BY_SIDE).matches;
    const drawn = listWithPending(
      client
        .getQueryData<InfiniteData<MessagePage>>(list)
        ?.pages.flatMap((p) => p.messages) ?? [],
      mailbox.id,
      pending,
    );
    const at = drawn.findIndex((x) => x.id === message);
    const neighbor =
      beside && at >= 0 ? (drawn[at + 1] ?? drawn[at - 1]) : undefined;
    // Read on the server before it moves, so it arrives read.
    const marked = read && m !== undefined && !m.seen;
    asked([
      ...(marked ? [draft({ kind: "seen", value: true, seen: false })] : []),
      draft(
        dest
          ? {
              kind: elsewhere ? "transfer" : "move",
              target: dest.id,
              ...(marked ? { seen: true } : {}),
            }
          : { kind: "delete" },
      ),
    ]);
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

  // In Scheduled a message is mail the server is to send: deleting it there cancels that, and
  // moving it anywhere would too, so cancelling is all that is offered.
  const scheduled = mailbox.special_use === "scheduled";
  const cancel = async () => {
    const yes = await confirm({
      title: "Cancel sending?",
      message: "It is deleted from Scheduled and never sent.",
      confirm: "Cancel sending",
      danger: true,
    });
    if (yes) moveTo(null);
  };
  const remove = async () => {
    if (scheduled) return cancel();
    if (to.trash) return moveTo(to.trash, false, readOn.trash);
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

  const star = () => ask({ kind: "flagged", value: !starred });
  const markSeen = () => ask({ kind: "seen", value: !seen });
  const act = scheduled
    ? { remove }
    : {
        archive: to.archive
          ? () => moveTo(to.archive!, false, readOn.archive)
          : undefined,
        remove,
        spam: to.spam ? () => moveTo(to.spam!) : undefined,
        notSpam: to.notSpam ? () => moveTo(to.notSpam!) : undefined,
        star,
        seen: markSeen,
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

  if (scheduled) {
    return (
      <div className={row}>
        {back}
        <Action
          className="ml-auto"
          label="Cancel sending…"
          keyName="#"
          disabled={busy}
          onClick={cancel}
        >
          <TrashMark />
        </Action>
      </div>
    );
  }

  return (
    <>
      <div className={row}>
        {back}
        <div className="ml-auto flex shrink-0 gap-1">
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
            onClick={star}
            pressed={starred}
          >
            <StarMark filled={starred} />
          </Action>
          <Action
            label={seen ? "Mark as unread" : "Mark as read"}
            keyName="u"
            disabled={busy}
            onClick={markSeen}
          >
            <MailMark open={seen} />
          </Action>
          <MovePicker
            config={config}
            boxes={boxes.data ?? []}
            current={mailbox}
            disabled={busy}
            onMove={moveTo}
            onTransfer={(dest) => moveTo(dest, true)}
          />
        </div>
      </div>
    </>
  );
}

/**
 * What leads, then the buttons, pushed right as one group. Where both do not fit, the buttons go
 * above rather than squeezing what leads: the lines wrap upward.
 */
const row = "flex flex-wrap-reverse items-center gap-2";

/** A button that is an icon, named for what it does, with its key in the tooltip. */
export function Action({
  label,
  keyName,
  pressed,
  children,
  ...props
}: {
  label: string;
  keyName?: string;
  pressed?: boolean;
  children: ReactNode;
  disabled?: boolean;
  className?: string;
  onClick: () => void;
}) {
  return (
    <Button
      size="bar"
      aria-label={label}
      aria-pressed={pressed}
      title={keyName ? `${label} (${keyName})` : label}
      {...props}
    >
      {children}
    </Button>
  );
}

/** The picker's choices that make a folder to move to, and that move to another account. */
const NEW = "new";
const ELSEWHERE = "elsewhere";

/** Where mail can be put: selectable, and not Scheduled, which the server refuses mail in. */
export const takes = (mb: Mailbox) =>
  mb.selectable && mb.special_use !== "scheduled";

/**
 * Moving messages of a folder to dest, another of its account's or another account's: each
 * unread one asked read first where read is set, all of them before any move, so each arrives
 * read. See docs/reading.md.
 */
export function moveJobs(
  config: string,
  mailbox: Mailbox,
  messages: Message[],
  dest: Mailbox,
  { elsewhere = false, read = false } = {},
): Pending[] {
  const job = (m: Message, what: Partial<Pending>): Pending => ({
    email_config: config,
    mailbox: mailbox.id,
    message: m.id,
    kind: "move",
    value: false,
    target: "",
    seen: m.seen,
    label: m.subject,
    ...what,
  });
  return [
    ...messages
      .filter((m) => read && !m.seen)
      .map((m) => job(m, { kind: "seen", value: true, seen: false })),
    ...messages.map((m) =>
      job(m, {
        kind: elsewhere ? "transfer" : "move",
        target: dest.id,
        ...(read ? { seen: true } : {}),
      }),
    ),
  ];
}

/**
 * Every other folder a message can be moved to, or a new one, and, with another account to move
 * it to, a dialog of theirs: the platform's own list, laid over a button that looks like the
 * others, so on a phone it is the system's picker and in a row of actions it is one more icon.
 */
export function MovePicker({
  config,
  boxes,
  current,
  disabled,
  onMove,
  onTransfer,
}: {
  config: string;
  boxes: Mailbox[];
  current: Mailbox;
  disabled: boolean;
  onMove: (to: Mailbox) => void;
  /** Moving to another account's folder. */
  onTransfer: (to: Mailbox) => void;
}) {
  const [making, setMaking] = useState(false);
  const [away, setAway] = useState(false);
  // Each opening starts the form afresh.
  const [opened, setOpened] = useState(0);
  const others = boxes.filter((mb) => takes(mb) && mb.id !== current.id);
  const configs = useQuery({
    queryKey: qk.emailConfigs,
    queryFn: getEmailConfigs,
  });
  const accounts = turnedOn(configs.data ?? []).filter((c) => c.id !== config);
  return (
    <>
      <label
        title="Move to folder"
        className={`${buttonLook("quiet", "bar")} relative focus-within:ring-1 focus-within:ring-brand ${disabled ? "opacity-50" : ""}`}
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
            if (e.target.value === ELSEWHERE) return setAway(true);
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
          {accounts.length > 0 ? (
            <option value={ELSEWHERE}>Another account…</option>
          ) : null}
        </select>
      </label>
      <ElsewhereDialog
        accounts={accounts}
        open={away}
        onClose={() => setAway(false)}
        onPick={(to) => {
          setAway(false);
          onTransfer(to);
        }}
      />
      {/* Beside the picker rather than in it: a click inside the dialog would be the label's. */}
      <FolderDialog
        key={opened}
        config={config}
        boxes={boxes}
        moving
        open={making}
        onClose={() => setMaking(false)}
        onDone={(made) => {
          setMaking(false);
          onMove(made);
        }}
      />
    </>
  );
}

/** The user's other accounts and their folders, to move to one: each account's under its name. */
function ElsewhereDialog({
  accounts,
  open,
  onClose,
  onPick,
}: {
  accounts: EmailConfig[];
  open: boolean;
  onClose: () => void;
  onPick: (to: Mailbox) => void;
}) {
  const theirs = useQueries({
    queries: accounts.map((c) => ({
      queryKey: qk.mailboxes(c.id),
      queryFn: () => getEmailConfigsByIdMailboxes(c.id),
      enabled: open,
    })),
  });
  return (
    <Dialog open={open} onClose={onClose} title="Move to another account">
      <div className="flex flex-col gap-4">
        {accounts.map((c, i) => {
          const boxes = theirs[i]?.data?.filter(takes);
          return (
            <section key={c.id} className="flex flex-col gap-1">
              <div className="flex items-baseline justify-between gap-3">
                <h3 className="caps shrink-0 text-sm text-muted">
                  {labelOf(c)}
                </h3>
                {/* Its address, when it goes by a name. */}
                {c.name ? (
                  <span className="min-w-0 truncate text-xs text-faint">
                    {c.email}
                  </span>
                ) : null}
              </div>
              {!boxes ? (
                <Dummy className="h-8 w-2/3" />
              ) : boxes.length === 0 ? (
                <p className="text-sm text-muted">No folders.</p>
              ) : (
                <ul aria-label={labelOf(c)} className="flex flex-col">
                  {boxes.map((mb) => (
                    <li key={mb.id}>
                      <button
                        type="button"
                        onClick={() => onPick(mb)}
                        className="flex min-h-8 w-full items-center rounded-md px-2 text-left text-sm hover:bg-shade"
                        style={{
                          paddingLeft: `${0.5 + depthOf(mb) * 0.875}rem`,
                        }}
                      >
                        {labelOfMailbox(mb)}
                      </button>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          );
        })}
      </div>
    </Dialog>
  );
}
