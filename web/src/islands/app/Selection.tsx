import { useEffect, useRef } from "react";
import type { InfiniteData } from "@tanstack/react-query";

import { useCached } from "@app/api/cached";
import { qk } from "@app/api/keys";
import type { JobDraft, Mailbox, Message, MessagePage } from "@app/api/types";
import { Button } from "@app/components/Button";
import { useConfirm } from "@app/components/Confirm";
import {
  ArchiveMark,
  Cross,
  InboxMark,
  MailMark,
  SpamMark,
  StarMark,
  TrashMark,
} from "@app/components/icons";
import { Action, MovePicker, targets } from "@app/islands/app/Actions";
import { useAsk } from "@app/islands/app/ask";
import { listWithPending, usePending } from "@app/islands/app/pending";

/**
 * The folder's header while messages are being selected: how many, all or none, and what can be
 * done to all of them at once — the reading pane's actions, each one request of jobs.
 */
export function Selection({
  config,
  mailbox,
  boxes,
  selected,
  change,
  done,
}: {
  config: string;
  mailbox: Mailbox;
  boxes: Mailbox[];
  selected: Set<string>;
  change: (next: Set<string>) => void;
  done: () => void;
}) {
  const ask = useAsk(config);
  const confirm = useConfirm();
  const pending = usePending(config);
  const listed = useCached<InfiniteData<MessagePage>>(
    qk.messages(config, mailbox.id),
  );
  // The list as drawn: a message on its way out is not there to select.
  const rows = listWithPending(
    listed?.pages.flatMap((p) => p.messages) ?? [],
    mailbox.id,
    pending,
  );
  const chosen = rows.filter((m) => selected.has(m.id));
  const all = rows.length > 0 && chosen.length === rows.length;
  const to = targets(boxes, mailbox);
  const none = chosen.length === 0;

  const job = (
    m: Message,
    what: Pick<JobDraft, "kind"> & Partial<JobDraft>,
  ) => ({
    email_config: config,
    mailbox: mailbox.id,
    message: m.id,
    value: false,
    target: "",
    seen: m.seen,
    label: m.subject,
    ...what,
  });
  const moveAll = (dest: Mailbox | null) => {
    ask(
      chosen.map((m) =>
        job(m, dest ? { kind: "move", target: dest.id } : { kind: "delete" }),
      ),
    );
    change(new Set());
  };
  const remove = async () => {
    if (to.trash) return moveAll(to.trash);
    const n = chosen.length;
    const yes = await confirm({
      title:
        n === 1
          ? "Delete this message for good?"
          : `Delete ${n} messages for good?`,
      message:
        mailbox.special_use === "trash"
          ? "They are removed from the server and cannot be brought back."
          : "This server has no Trash folder, so they cannot be brought back.",
      confirm: "Delete forever",
      danger: true,
    });
    if (yes) moveAll(null);
  };
  // Read if any is unread, else unread; starred if any is not, else not: what a mixed
  // selection most likely means.
  const unread = chosen.some((m) => !m.seen);
  const unstarred = chosen.some((m) => !m.flagged);
  const act = {
    archive: to.archive ? () => moveAll(to.archive!) : undefined,
    remove,
    spam: to.spam ? () => moveAll(to.spam!) : undefined,
    notSpam: to.notSpam ? () => moveAll(to.notSpam!) : undefined,
    star: () =>
      ask(
        chosen
          .filter((m) => !unstarred || !m.flagged)
          .map((m) => job(m, { kind: "flagged", value: unstarred })),
      ),
    seen: () =>
      ask(
        chosen
          .filter((m) => !unread || !m.seen)
          .map((m) => job(m, { kind: "seen", value: unread })),
      ),
  };

  // The reading pane's keys, for the selection; Escape leaves.
  const latest = useRef({ act, none, done });
  latest.current = { act, none, done };
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const now = latest.current;
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const el = e.target instanceof HTMLElement ? e.target : null;
      if (el?.closest("input:not([type=checkbox]), textarea, select")) return;
      if (document.querySelector("dialog[open]")) return;
      if (e.key === "Escape") {
        e.preventDefault();
        now.done();
        return;
      }
      if (now.none) return;
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
    <div className="flex min-w-0 flex-1 flex-wrap items-center gap-1">
      <Button
        size="bar"
        aria-label="Done selecting"
        title="Done (Esc)"
        onClick={done}
      >
        <Cross />
      </Button>
      <label className="flex items-center gap-2 px-1 text-sm">
        <input
          type="checkbox"
          aria-label="Select all"
          checked={all}
          ref={(el) => {
            if (el) el.indeterminate = !all && !none;
          }}
          onChange={() =>
            change(all ? new Set() : new Set(rows.map((m) => m.id)))
          }
        />
        {/* The number alone, so the actions fit one line on a phone and in the list pane. */}
        <span>{chosen.length}</span>
        <span className="sr-only"> selected</span>
      </label>
      <span className="flex-1" />
      {act.archive ? (
        <Action
          label="Archive"
          keyName="e"
          disabled={none}
          onClick={act.archive}
        >
          <ArchiveMark />
        </Action>
      ) : null}
      <Action
        label={to.trash ? "Delete" : "Delete forever…"}
        keyName="#"
        disabled={none}
        onClick={act.remove}
      >
        <TrashMark />
      </Action>
      {act.spam ? (
        <Action label="Spam" keyName="!" disabled={none} onClick={act.spam}>
          <SpamMark />
        </Action>
      ) : null}
      {act.notSpam ? (
        <Action
          label="Not spam"
          keyName="!"
          disabled={none}
          onClick={act.notSpam}
        >
          <InboxMark />
        </Action>
      ) : null}
      <Action
        label={unstarred || none ? "Star" : "Unstar"}
        keyName="s"
        disabled={none}
        onClick={act.star}
      >
        <StarMark filled={!none && !unstarred} />
      </Action>
      <Action
        label={unread || none ? "Mark as read" : "Mark as unread"}
        keyName="u"
        disabled={none}
        onClick={act.seen}
      >
        <MailMark open={!unread && !none} />
      </Action>
      <MovePicker
        boxes={boxes}
        current={mailbox}
        disabled={none}
        onMove={moveAll}
      />
    </div>
  );
}
