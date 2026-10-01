import { useMutationState } from "@tanstack/react-query";

import { mk } from "@app/api/keys";
import type { Mailbox, Message } from "@app/api/types";

/**
 * Actions somebody has pressed and the server has not yet answered, drawn over what the server
 * last said rather than written into it.
 *
 * The cache holds only what the server has said or confirmed. A refusal leaves nothing to undo:
 * the action stops being pending and the screen is the server's again, whatever was pressed
 * after it. And a list read back while actions are queued is drawn with them still applied.
 */
export type Change = { seen?: boolean; flagged?: boolean };

export type Pending =
  | {
      kind: "flags";
      mailbox: string;
      message: string;
      change: Change;
      /** Whether it was read when pressed, as drawn then: what the unread count moves by. */
      seen: boolean;
    }
  | {
      kind: "move";
      mailbox: string;
      message: string;
      /** The mailbox it goes to, null for deleted for good. */
      to: string | null;
      seen: boolean;
    };

/** One config's pending actions, in the order pressed. */
export function usePending(config: string): Pending[] {
  return useMutationState({
    filters: { mutationKey: mk.actionsOf(config), status: "pending" },
    select: (m) => m.state.variables as Pending,
  });
}

/** A message as it is once its pending flag changes are done. */
export function withPending<T extends Pick<Message, "id" | "seen" | "flagged">>(
  m: T,
  mailbox: string,
  pending: Pending[],
): T {
  let out = m;
  for (const p of pending) {
    if (p.kind === "flags" && p.mailbox === mailbox && p.message === m.id) {
      out = { ...out, ...p.change };
    }
  }
  return out;
}

/** A mailbox's list as it is once its pending actions are done: moved ones gone, flags changed. */
export function listWithPending(
  msgs: Message[],
  mailbox: string,
  pending: Pending[],
): Message[] {
  const gone = new Set(
    pending
      .filter((p) => p.kind === "move" && p.mailbox === mailbox)
      .map((p) => p.message),
  );
  return msgs
    .filter((m) => !gone.has(m.id))
    .map((m) => withPending(m, mailbox, pending));
}

/** What an action does to the mailboxes' counts: each mailbox, and by how much. */
export function effects(p: Pending): [string, number, number][] {
  const unseen = p.seen ? 0 : 1;
  if (p.kind === "move") {
    const out: [string, number, number][] = [[p.mailbox, -1, -unseen]];
    if (p.to) out.push([p.to, 1, unseen]);
    return out;
  }
  if (p.change.seen === undefined || p.change.seen === p.seen) return [];
  return [[p.mailbox, 0, p.change.seen ? -1 : 1]];
}

/** Mailboxes moved by the given effects. */
export function counted(
  boxes: Mailbox[],
  moves: [string, number, number][],
): Mailbox[] {
  if (moves.length === 0) return boxes;
  return boxes.map((mb) => {
    let messages = mb.messages;
    let unseen = mb.unseen;
    for (const [id, dm, du] of moves) {
      if (id !== mb.id) continue;
      messages += dm;
      unseen += du;
    }
    return messages === mb.messages && unseen === mb.unseen
      ? mb
      : { ...mb, messages: Math.max(0, messages), unseen: Math.max(0, unseen) };
  });
}

/** The mailboxes' counts as they are once the pending actions are done. */
export function countsWithPending(
  boxes: Mailbox[],
  pending: Pending[],
): Mailbox[] {
  return counted(boxes, pending.flatMap(effects));
}
