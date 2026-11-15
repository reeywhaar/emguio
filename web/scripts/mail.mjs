// A mail server filled with invented mail, for a run of emguio with nobody's real mail in it: the
// README's screenshots, and the demo in demo/. A connection one command at a time, and a message
// as a server stores it.
//
// A message is { key, folder, from, to?, subject, ago, body, ... }:
//   ago          how long before now it arrived: 25m, 3h, 2d, 1w
//   unread       not \Seen; flagged is \Flagged
//   replyTo      the key of the message it answers, which puts it in that conversation
//   attachments  [{ name, type, size }], filled with bytes of that size
//   html         an HTML part beside the text one
//   unsubscribe  the web address it says to unsubscribe at, in one click: a newsletter
// `to` is the mailbox's owner when it is left out.
import { randomBytes } from "node:crypto";
import { connect } from "node:tls";

/** A connection to a mail server, one command at a time. */
export class Imap {
  #socket;
  #lines = [];
  #waiting = null;
  #buffer = "";
  #next = 1;

  /** Signed in to nothing yet; servername is the name its certificate is for. */
  static open({ host = "127.0.0.1", port, servername, ca }) {
    return new Promise((done, fail) => {
      const imap = new Imap();
      imap.#socket = connect({ host, port, servername, ca });
      imap.#socket.setEncoding("utf8");
      imap.#socket.on("data", (chunk) => imap.#read(chunk));
      imap.#socket.once("error", fail);
      imap
        .#line()
        .then((greeting) =>
          greeting.startsWith("* OK") ? done(imap) : fail(new Error(greeting)),
        );
    });
  }

  #read(chunk) {
    this.#buffer += chunk;
    for (let at; (at = this.#buffer.indexOf("\r\n")) >= 0;) {
      this.#lines.push(this.#buffer.slice(0, at));
      this.#buffer = this.#buffer.slice(at + 2);
    }
    if (this.#waiting && this.#lines.length) {
      const waiting = this.#waiting;
      this.#waiting = null;
      waiting(this.#lines.shift());
    }
  }

  #line() {
    if (this.#lines.length) return Promise.resolve(this.#lines.shift());
    return new Promise((done) => (this.#waiting = done));
  }

  /**
   * Sends a command and waits for its answer, saying what the server said before it; literal is
   * sent once the server asks for it. A refusal throws, saying the server's line, unless already
   * allows a folder made before.
   */
  async command(text, { literal, already = false } = {}) {
    const tag = `t${this.#next++}`;
    this.#socket.write(`${tag} ${text}\r\n`);
    const said = [];
    for (;;) {
      const line = await this.#line();
      if (line.startsWith("+") && literal) {
        this.#socket.write(Buffer.concat([literal, Buffer.from("\r\n")]));
        continue;
      }
      if (!line.startsWith(`${tag} `)) {
        said.push(line);
        continue;
      }
      if (line.startsWith(`${tag} OK`)) return said;
      if (already && line.includes("ALREADYEXISTS")) return said;
      throw new Error(`the mail server refused ${text.split(" ")[0]}: ${line}`);
    }
  }

  /** How many messages folder holds. */
  async count(folder) {
    const said = await this.command(`STATUS ${quote(folder)} (MESSAGES)`);
    return Number(/MESSAGES (\d+)/.exec(said.join(" "))?.[1] ?? 0);
  }

  /** Appends raw to folder with flags, dated at. */
  append(folder, flags, at, raw) {
    return this.command(
      `APPEND ${quote(folder)} (${flags.join(" ")}) ${quote(internalDate(at))} {${raw.length}}`,
      { literal: raw },
    );
  }

  close() {
    this.#socket.end();
  }
}

/**
 * Signs in as owner and fills their mailbox: folders made, then every message appended oldest
 * first, dated how long before now it arrived. Message-IDs are the messages' keys @domain.
 */
export async function fill(imap, owner, { folders, messages, now, domain }) {
  await imap.command(`LOGIN ${quote(owner.email)} ${quote(owner.password)}`);
  for (const folder of folders)
    await imap.command(`CREATE ${quote(folder)}`, { already: true });
  const at = (ago) => new Date(now - seconds(ago) * 1000);
  const keyed = Object.fromEntries(messages.map((m) => [m.key, m]));
  const id = (key) => `<${key}@${domain}>`;
  // Oldest first, as real mail comes: a list is in the order of the dates APPEND gives.
  const arrival = messages.toSorted((a, b) => seconds(b.ago) - seconds(a.ago));
  for (const m of arrival) {
    const flags = [
      ...(m.unread ? [] : ["\\Seen"]),
      ...(m.flagged ? ["\\Flagged"] : []),
      ...(m.folder === "Drafts" ? ["\\Draft"] : []),
    ];
    const thread = [];
    for (let up = m.replyTo; up; up = keyed[up]?.replyTo)
      thread.unshift(id(up));
    const raw = Buffer.from(
      stored(m, {
        to: `${owner.name} <${owner.email}>`,
        date: at(m.ago),
        id: id(m.key),
        thread,
      }),
      "utf8",
    );
    await imap.append(m.folder, flags, at(m.ago), raw);
  }
}

/** The message as the server stores it: headers, then its text, and its parts if it has any. */
export function stored(m, { to, date, id, thread = [] }) {
  const head = [
    `From: ${m.from}`,
    `To: ${m.to ?? to}`,
    `Subject: ${m.subject}`,
    `Date: ${date.toUTCString()}`,
    `Message-ID: ${id}`,
    ...(thread.length
      ? [`In-Reply-To: ${thread.at(-1)}`, `References: ${thread.join(" ")}`]
      : []),
    ...(m.unsubscribe
      ? [
          `List-Unsubscribe: <${m.unsubscribe}>`,
          "List-Unsubscribe-Post: List-Unsubscribe=One-Click",
        ]
      : []),
    "MIME-Version: 1.0",
  ];
  const text = [
    "Content-Type: text/plain; charset=utf-8",
    "Content-Transfer-Encoding: 8bit",
    "",
    m.body,
  ].join("\r\n");
  let body = text;
  if (m.html) {
    body = multipart("alternative", [
      text,
      [
        "Content-Type: text/html; charset=utf-8",
        "Content-Transfer-Encoding: 8bit",
        "",
        `<!doctype html><html><body>${m.html}</body></html>`,
      ].join("\r\n"),
    ]);
  }
  if (m.attachments?.length) {
    body = multipart("mixed", [
      body,
      ...m.attachments.map((a) =>
        [
          `Content-Type: ${a.type}; name="${a.name}"`,
          `Content-Disposition: attachment; filename="${a.name}"`,
          "Content-Transfer-Encoding: base64",
          "",
          filler(a.size).toString("base64").replace(/.{76}/g, "$&\r\n"),
        ].join("\r\n"),
      ),
    ]);
  }
  return [...head, body].join("\r\n").replace(/\r?\n/g, "\r\n");
}

function multipart(kind, parts) {
  const boundary = `=_${kind}_${randomBytes(6).toString("hex")}`;
  return [
    `Content-Type: multipart/${kind}; boundary="${boundary}"`,
    "",
    ...parts.flatMap((part) => [`--${boundary}`, part]),
    `--${boundary}--`,
    "",
  ].join("\r\n");
}

/** Bytes standing in for a file of that size. Nobody opens them. */
function filler(size) {
  return Buffer.alloc(size, "emguio invented mail ");
}

/** How long a span is: 25m, 3h, 2d, 1w. */
export function seconds(span) {
  const [, n, unit] = /^(\d+)([mhdw])$/.exec(span) ?? [];
  if (!n) throw new Error(`not a span: ${span}`);
  return Number(n) * { m: 60, h: 3600, d: 86400, w: 604800 }[unit];
}

/** A time as APPEND takes it: 17-Jun-2026 10:05:00 +0000. */
export function internalDate(d) {
  const months = "Jan Feb Mar Apr May Jun Jul Aug Sep Oct Nov Dec".split(" ");
  const two = (n) => String(n).padStart(2, "0");
  return `${two(d.getUTCDate())}-${months[d.getUTCMonth()]}-${d.getUTCFullYear()} ${two(d.getUTCHours())}:${two(d.getUTCMinutes())}:${two(d.getUTCSeconds())} +0000`;
}

export function quote(s) {
  return `"${s.replace(/[\\"]/g, "\\$&")}"`;
}
