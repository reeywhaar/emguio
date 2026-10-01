import { useSyncExternalStore } from "react";

import type {
  Address,
  Held,
  KeptDraft,
  Part,
  ReadMessage,
} from "@app/api/types";
import { full } from "@app/format";

/**
 * A message being written: one at a time, for the whole island, so the reading pane can start a
 * reply and the list a new one without either holding the other's state.
 */
export type Draft = {
  config: string;
  /** As typed: addresses apart by commas. */
  to: string;
  cc: string;
  bcc: string;
  subject: string;
  text: string;
  /** Files from this device, not yet kept in emguio. */
  files: File[];
  /** The message it answers. */
  reply: Ref | null;
  /**
   * A message on the mail server some of whose parts go with it — the one it forwards, or the
   * draft it was opened from — until emguio keeps them.
   */
  carry: (Ref & { parts: Part[] }) | null;
  /** The draft on the mail server it was opened from. */
  draft: Ref | null;
  /** The draft kept in emguio, once saved, and the attachments it holds. */
  id: string | null;
  held: Held[];
};

type Ref = { mailbox: string; message: string };

/** A draft opened, numbered so that another opening is another window rather than this one. */
export type Writing = { id: number; draft: Draft };

let current: Writing | null = null;
let opened = 0;
const listeners = new Set<() => void>();

function publish(writing: Writing | null) {
  current = writing;
  for (const listener of listeners) listener();
}

/** Opens the compose window on draft, in place of whatever was being written. */
export const write = (draft: Draft) => publish({ id: ++opened, draft });
export const stopWriting = () => publish(null);

export function useWriting(): Writing | null {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    () => current,
  );
}

export const blank = (config: string): Draft => ({
  config,
  to: "",
  cc: "",
  bcc: "",
  subject: "",
  text: "",
  files: [],
  reply: null,
  carry: null,
  draft: null,
  id: null,
  held: [],
});

/** An address as typed into a field: quoted where its name holds what would split it. */
export function typed(a: Address): string {
  if (!a.name) return a.email;
  const name = /[,;"<>@()[\]:\\]/.test(a.name)
    ? `"${a.name.replace(/["\\]/g, "\\$&")}"`
    : a.name;
  return `${name} <${a.email}>`;
}

const same = (a: Address, b: Address) =>
  a.email.toLowerCase() === b.email.toLowerCase();

/** Each address once, without any of not. */
function only(list: Address[], not: Address[]): Address[] {
  const out: Address[] = [];
  for (const a of list) {
    if (
      !a.email ||
      not.some((n) => same(a, n)) ||
      out.some((o) => same(a, o))
    ) {
      continue;
    }
    out.push(a);
  }
  return out;
}

/** A subject prefixed once, however many replies it has been through in other clients. */
function prefixed(prefix: string, already: RegExp, subject: string) {
  return already.test(subject) ? subject : `${prefix} ${subject}`.trim();
}

/** The message's text, or its HTML as text, which is what is quoted and passed on. */
const textOf = (m: ReadMessage) => (m.text || m.html_text).replace(/\s+$/, "");

/**
 * A reply: to where the sender asks replies to go, or with all, to everybody else it went to as
 * well — never to self. One's own message, in Sent say, is replied to its recipients. The text
 * starts empty above the quote, where the cursor goes.
 */
export function replyTo(
  m: ReadMessage,
  config: string,
  self: string,
  all: boolean,
): Draft {
  const me = { name: "", email: self };
  const mine = same(m.from, me);
  const sender = m.reply_to.length > 0 ? m.reply_to : [m.from];
  const to = only(mine ? m.to : all ? [...sender, ...m.to] : sender, [me]);
  const cc = all ? only(m.cc, [me, ...to]) : [];
  const said = m.sent ?? m.date;
  const quoted = textOf(m)
    .split("\n")
    .map((line) => (line ? `> ${line}` : ">"))
    .join("\n");
  return {
    ...blank(config),
    to: to.map(typed).join(", "),
    cc: cc.map(typed).join(", "),
    subject: prefixed("Re:", /^(re|aw|sv|antw)\s*:/i, m.subject),
    text: `\n\nOn ${full(said)}, ${typed(m.from)} wrote:\n${quoted}\n`,
    reply: { mailbox: m.mailbox, message: m.id },
  };
}

/** A forward: the message's text below a line saying what it was, and its attachments with it. */
export function forwardOf(m: ReadMessage, config: string): Draft {
  const head = [
    "---------- Forwarded message ----------",
    `From: ${typed(m.from)}`,
    `Date: ${full(m.sent ?? m.date)}`,
    `Subject: ${m.subject}`,
    m.to.length ? `To: ${m.to.map(typed).join(", ")}` : "",
    m.cc.length ? `Cc: ${m.cc.map(typed).join(", ")}` : "",
  ].filter(Boolean);
  return {
    ...blank(config),
    subject: prefixed("Fwd:", /^(fwd?|fw)\s*:/i, m.subject),
    text: `\n\n${head.join("\n")}\n\n${textOf(m)}\n`,
    carry: {
      mailbox: m.mailbox,
      message: m.id,
      parts: m.parts.filter((p) => p.listed),
    },
  };
}

/** Whether a reply to all would reach anybody a reply would not. */
export function others(m: ReadMessage, self: string): boolean {
  const me = { name: "", email: self };
  const sender = m.reply_to.length > 0 ? m.reply_to : [m.from];
  return only([...m.to, ...m.cc], [me, ...sender]).length > 0;
}

/** A draft opened again from Drafts, to be written on and saved in place of itself. */
export function resumed(m: ReadMessage, config: string): Draft {
  const at = { mailbox: m.mailbox, message: m.id };
  return {
    ...blank(config),
    to: m.to.map(typed).join(", "),
    cc: m.cc.map(typed).join(", "),
    bcc: m.bcc.map(typed).join(", "),
    subject: m.subject,
    text: m.text || m.html_text,
    carry: { ...at, parts: m.parts.filter((p) => p.listed) },
    draft: at,
  };
}

/** Whether a has anything b does not, or b anything a does not. */
export function differs(a: Draft, b: Draft): boolean {
  const parts = (d: Draft) =>
    [
      ...d.held.map((h) => h.id),
      ...(d.carry
        ? [d.carry.message, ...d.carry.parts.map((p) => p.section)]
        : []),
    ].join(" ");
  return (
    a.to !== b.to ||
    a.cc !== b.cc ||
    a.bcc !== b.bcc ||
    a.subject !== b.subject ||
    a.text !== b.text ||
    a.files.length !== b.files.length ||
    a.files.some((f, i) => f !== b.files[i]) ||
    parts(a) !== parts(b)
  );
}

/**
 * now, once saved was kept in emguio as kept: what it held, what it carried and its files are
 * held there from then on, in that order. What was removed while it was being kept stays
 * removed, and files added meanwhile wait for the next save.
 */
export function settled(now: Draft, saved: Draft, kept: KeptDraft): Draft {
  const still = [
    ...saved.held.map((h) => now.held.some((x) => x.id === h.id)),
    ...(saved.carry?.parts ?? []).map(
      (p) => now.carry?.parts.some((q) => q.section === p.section) ?? false,
    ),
    ...saved.files.map((f) => now.files.includes(f)),
  ];
  return {
    ...now,
    id: kept.id,
    held: kept.parts.filter((_, i) => still[i]),
    carry: null,
    draft: null,
    files: now.files.filter((f) => !saved.files.includes(f)),
  };
}
