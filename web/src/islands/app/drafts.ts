import { useSyncExternalStore } from "react";

import type { Address, Part, ReadMessage } from "@app/api/types";
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
  /** Files from this device. */
  files: File[];
  /** The message it answers. */
  reply: { mailbox: string; message: string } | null;
  /** The message it passes on, and the parts of it that go with it. */
  forward: { mailbox: string; message: string; parts: Part[] } | null;
};

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
  forward: null,
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
    forward: {
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
