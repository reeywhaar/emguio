import type { DragEvent } from "react";

import type { Mailbox, Message } from "@app/api/types";

/** A mouse's or a pen's: what drags mail. A finger presses, and scrolls the list. */
export const CURSOR = "(pointer: fine)";

/** Messages of one folder carried out of its list, to be dropped on another folder. */
export type Dragged = {
  config: string;
  mailbox: Mailbox;
  q: string;
  messages: Message[];
  /** What the list does once they are dropped: lets go of the selection they were. */
  dropped?: () => void;
};

// Kept here as well as in the drag: a page cannot read the drag's own data until the drop.
let carried: Dragged | null = null;

/** What is being dragged, if it is mail. */
export const dragged = () => carried;

const TYPE = "application/x-emguio-messages";

/** Whether what is over the page is mail, which is all a folder asks before the drop. */
export const isMail = (e: DragEvent) => e.dataTransfer.types.includes(TYPE);

/** The drag of these messages begins, under a label saying what they are. */
export function startDrag(e: DragEvent, d: Dragged) {
  carried = d;
  e.dataTransfer.effectAllowed = "move";
  e.dataTransfer.setData(TYPE, d.messages.map((m) => m.id).join(","));
  const label = document.createElement("div");
  label.textContent =
    d.messages.length === 1
      ? d.messages[0]!.subject || "1 message"
      : `${d.messages.length} messages`;
  label.className =
    "fixed -top-96 left-0 max-w-64 truncate rounded-md border border-line bg-bg px-3 py-1.5 text-sm text-fg shadow-lg";
  document.body.append(label);
  e.dataTransfer.setDragImage(label, 12, 12);
  // Drawn into the drag once, when it starts.
  setTimeout(() => label.remove());
}

export function endDrag() {
  carried = null;
}
