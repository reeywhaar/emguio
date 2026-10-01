import type { Mailbox, Message } from "@app/api/types";

/** What a mailbox is called in a sidebar: its own last name, and INBOX as a word. */
export function labelOfMailbox(mb: Mailbox): string {
  if (mb.special_use === "inbox" && mb.path.length === 1) return "Inbox";
  return mb.path[mb.path.length - 1] ?? mb.name;
}

/** How deep in the tree a mailbox sits, from 0 at the top. */
export function depthOf(mb: Mailbox): number {
  return mb.path.length - 1;
}

/** Whether a's path starts with all of b's, and is longer: a is somewhere inside b. */
export const within = (a: Mailbox, b: Mailbox) =>
  a.path.length > b.path.length && b.path.every((p, i) => a.path[i] === p);

/** Whether the server keeps mb for a purpose, or mb holds one it does: it keeps its place. */
export const serversOwn = (boxes: Mailbox[], mb: Mailbox) =>
  mb.special_use !== "" ||
  boxes.some((other) => other.special_use !== "" && within(other, mb));

/** The mailboxes side by side with mb, mb among them, in the order listed. */
export function besideOf(boxes: Mailbox[], mb: Mailbox): Mailbox[] {
  const depth = mb.path.length - 1;
  return boxes.filter(
    (other) =>
      other.path.length === mb.path.length &&
      mb.path.slice(0, depth).every((p, i) => other.path[i] === p),
  );
}

/**
 * The tree with mailboxes side by side put in the order of ids, each taking what is inside it
 * along.
 */
export function arranged(boxes: Mailbox[], ids: string[]): Mailbox[] {
  const blocks = ids.flatMap((id) => {
    const top = boxes.find((mb) => mb.id === id);
    return top ? boxes.filter((mb) => mb === top || within(mb, top)) : [];
  });
  const moved = new Set(blocks);
  const at = boxes.findIndex((mb) => moved.has(mb));
  if (at < 0) return boxes;
  const rest = boxes.filter((mb) => !moved.has(mb));
  return [...rest.slice(0, at), ...blocks, ...rest.slice(at)];
}

/** The mailbox a view opens on when none is named: INBOX, or the first that can be opened. */
export function usual(boxes: Mailbox[]): Mailbox | undefined {
  return (
    boxes.find((mb) => mb.special_use === "inbox") ??
    boxes.find((mb) => mb.selectable)
  );
}

/**
 * Who a row is about. In mail one sent, that is who it went to; everywhere else, who it came
 * from — the name when there is one, the address when there is not.
 */
export function counterpart(mb: Mailbox, m: Message): string {
  if (mb.special_use === "sent" || mb.special_use === "drafts") {
    const to = m.to[0];
    if (!to) return "(no recipient)";
    return `To: ${to.name || to.email}${m.to.length > 1 ? ` +${m.to.length - 1}` : ""}`;
  }
  return m.from.name || m.from.email || "(no sender)";
}
