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
