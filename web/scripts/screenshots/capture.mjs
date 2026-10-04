#!/usr/bin/env node
//
// Regenerate the README screenshots.
//
//   npm --prefix web run screenshots
//
// Builds the frontend and a Linux binary, and starts in Docker a mail server holding seed.mjs's
// mail as imap.example.com, a stand-in that answers as smtp.example.com and publishes their
// settings as autoconfig.example.com, and an emguio on an empty data directory. Then walks the install in headless Chromium over the
// DevTools protocol — the invitation, the mail account, the mail — and overwrites the PNGs in
// docs/screenshots. Everything it starts is stopped on the way out.
//
//   SCALE=2      device pixels per CSS pixel
//   THEME=light  or dark
//   OUT=dir      where the PNGs land, so a look at dark need not overwrite the committed set
//   ONLY=a,b     just these shots
//   NOTES=0      without the notes
//   TIMEOUT=30   seconds a shot may take before the run gives up, cleaning up as it goes
//
// Needs go, node, docker, openssl and Chromium or Chrome.
import { execFileSync, spawnSync } from "node:child_process";
import {
  chmodSync,
  cpSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  rmSync,
  readFileSync,
  writeFileSync,
} from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";
import { connect } from "node:tls";
import { fileURLToPath } from "node:url";
import { randomBytes } from "node:crypto";
import { crc32, inflateSync } from "node:zlib";

import { Browser, findChromium, waitFor } from "../cdp.mjs";
import { account, folders, mail, messages, now, opened } from "./seed.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, "..", "..", "..");
const work = mkdtempSync(join(tmpdir(), "emguio-shots-"));

const SCALE = Number(process.env.SCALE ?? 2);
const THEME = process.env.THEME ?? "light";
const OUT = process.env.OUT ?? join(root, "docs", "screenshots");
const ONLY = (process.env.ONLY ?? "").split(",").filter(Boolean);
const NOTES = process.env.NOTES !== "0";
const TIMEOUT = Number(process.env.TIMEOUT ?? 30);

const DOVECOT = "dovecot/dovecot:latest";
const NODE = "node:26-alpine";
const ALPINE = "alpine:3";
// Names for what is started here, unique to the run so two never trip over each other.
const name = `emguio-shots-${randomBytes(3).toString("hex")}`;

const started = [];
let network = false;
let browser;
function cleanup() {
  browser?.close();
  for (const container of started)
    spawnSync("docker", ["rm", "-f", container], { stdio: "ignore" });
  if (network)
    spawnSync("docker", ["network", "rm", name], { stdio: "ignore" });
  // Chromium is still writing its profile as it exits.
  rmSync(work, {
    recursive: true,
    force: true,
    maxRetries: 5,
    retryDelay: 300,
  });
}
process.on("exit", cleanup);
process.on("SIGINT", () => process.exit(1));
process.on("SIGTERM", () => process.exit(1));

const step = (message) => console.log(`==> ${message}`);
function die(message, detail) {
  console.error(message);
  if (detail) console.error(detail);
  process.exit(1);
}

function run(command, args, options = {}) {
  try {
    return execFileSync(command, args, {
      stdio: "pipe",
      encoding: "utf8",
      ...options,
    });
  } catch (err) {
    die(
      `${command} ${args.join(" ")} failed`,
      [err.stdout, err.stderr].join("\n"),
    );
  }
}
const docker = (...args) => run("docker", args).trim();

// --- build ---------------------------------------------------------------------------------

const chromium = findChromium();
if (!chromium) die("no Chromium or Chrome found; set CHROMIUM");
docker("version", "--format", "{{.Server.Version}}");

step("building the frontend");
if (!existsSync(join(root, "web", "node_modules")))
  run("npm", ["ci"], { cwd: join(root, "web") });
run("npm", ["run", "build"], { cwd: join(root, "web") });

// For the Linux Docker runs, whatever this machine is: on macOS a binary of its own trusts only
// the system's certificates, and this mail server's are made up a moment from now.
step("building the binary");
const app = join(work, "app");
mkdirSync(join(app, "web"), { recursive: true });
run("go", ["build", "-trimpath", "-o", join(app, "emguio"), "."], {
  cwd: root,
  env: {
    ...process.env,
    GOOS: "linux",
    GOARCH: docker("version", "--format", "{{.Server.Arch}}"),
    CGO_ENABLED: "0",
  },
});
cpSync(join(root, "web", "dist"), join(app, "web", "dist"), {
  recursive: true,
});

// --- example.com -----------------------------------------------------------------------------

step("making certificates for example.com");
const certs = join(work, "certs");
mkdirSync(certs);
const openssl = (...args) => run("openssl", args, { cwd: certs });
openssl(
  "req",
  "-x509",
  "-newkey",
  "rsa:2048",
  "-nodes",
  "-days",
  "2",
  "-subj",
  "/CN=emguio screenshots",
  "-addext",
  "basicConstraints=critical,CA:TRUE",
  "-addext",
  "keyUsage=critical,keyCertSign",
  "-keyout",
  "ca.key",
  "-out",
  "ca.pem",
);
openssl(
  "req",
  "-newkey",
  "rsa:2048",
  "-nodes",
  "-subj",
  "/CN=imap.example.com",
  "-keyout",
  "host.key",
  "-out",
  "host.csr",
);
writeFileSync(
  join(certs, "host.ext"),
  "subjectAltName=DNS:imap.example.com,DNS:smtp.example.com,DNS:autoconfig.example.com\n",
);
openssl(
  "x509",
  "-req",
  "-days",
  "2",
  "-in",
  "host.csr",
  "-CA",
  "ca.pem",
  "-CAkey",
  "ca.key",
  "-CAcreateserial",
  "-extfile",
  "host.ext",
  "-out",
  "host.pem",
);
// Read inside the containers by users that are not this one.
for (const file of ["ca.pem", "host.pem", "host.key"])
  chmodSync(join(certs, file), 0o644);

step("starting the mail server");
docker("network", "create", name);
network = true;
// The usual port rather than the image's own, so the account's settings read as anybody's.
writeFileSync(
  join(work, "ports.conf"),
  `service imap-login {
  inet_listener imaps {
    port = 993
  }
}
`,
);
const container = (role, image, options, command = []) => {
  const id = `${name}-${role}`;
  started.push(id);
  docker(
    "run",
    "-d",
    "--name",
    id,
    "--network",
    name,
    ...options,
    image,
    ...command,
  );
  return id;
};
const mailserver = container("mail", DOVECOT, [
  "-e",
  `USER_PASSWORD=${mail.password}`,
  "--sysctl",
  "net.ipv4.ip_unprivileged_port_start=0",
  "-v",
  `${join(certs, "host.pem")}:/etc/dovecot/ssl/tls.crt:ro`,
  "-v",
  `${join(certs, "host.key")}:/etc/dovecot/ssl/tls.key:ro`,
  "-v",
  `${join(work, "ports.conf")}:/etc/dovecot/conf.d/shots.conf:ro`,
  "-p",
  "127.0.0.1::993",
]);

// What a provider publishes for Thunderbird, which is the first place emguio looks; and the
// outgoing server it names, as far as a test of it goes: greeted, signed in to, left. Dovecot's
// own has nowhere to relay to, and says so as an internal error.
const settings = join(work, "autoconfig");
mkdirSync(settings);
cpSync(join(certs, "host.pem"), join(settings, "host.pem"));
cpSync(join(certs, "host.key"), join(settings, "host.key"));
writeFileSync(
  join(settings, "config-v1.1.xml"),
  `<?xml version="1.0"?>
<clientConfig version="1.1">
  <emailProvider id="example.com">
    <domain>example.com</domain>
    <incomingServer type="imap">
      <hostname>imap.example.com</hostname>
      <port>993</port>
      <socketType>SSL</socketType>
      <username>%EMAILADDRESS%</username>
      <authentication>password-cleartext</authentication>
    </incomingServer>
    <outgoingServer type="smtp">
      <hostname>smtp.example.com</hostname>
      <port>465</port>
      <socketType>SSL</socketType>
      <username>%EMAILADDRESS%</username>
      <authentication>password-cleartext</authentication>
    </outgoingServer>
  </emailProvider>
</clientConfig>
`,
);
writeFileSync(
  join(settings, "serve.mjs"),
  `import { createServer } from "node:https";
import { createServer as tls } from "node:tls";
import { readFileSync } from "node:fs";
const keys = { cert: readFileSync("/srv/host.pem"), key: readFileSync("/srv/host.key") };
const xml = readFileSync("/srv/config-v1.1.xml");
createServer(keys, (req, res) => {
  if (req.url !== "/mail/config-v1.1.xml") return res.writeHead(404).end();
  res.writeHead(200, { "Content-Type": "text/xml" }).end(xml);
}).listen(443);
tls(keys, (socket) => {
  const say = (line) => socket.write(line + "\\r\\n");
  // What the next line is: a command, or a step of signing in.
  let expect = "command";
  let buffer = "";
  say("220 smtp.example.com ESMTP");
  socket.on("data", (chunk) => {
    buffer += chunk;
    for (let at; (at = buffer.indexOf("\\r\\n")) >= 0; ) {
      const line = buffer.slice(0, at);
      buffer = buffer.slice(at + 2);
      const [verb = "", kind = "", initial] = line.toUpperCase().split(" ");
      if (expect === "username") {
        expect = "password";
        say("334 UGFzc3dvcmQ6");
      } else if (expect !== "command") {
        expect = "command";
        say("235 2.7.0 Signed in");
      } else if (verb === "EHLO") say("250-smtp.example.com\\r\\n250 AUTH PLAIN LOGIN");
      else if (verb === "AUTH" && kind === "LOGIN") {
        expect = "username";
        say("334 VXNlcm5hbWU6");
      } else if (verb === "AUTH" && initial) say("235 2.7.0 Signed in");
      else if (verb === "AUTH") {
        expect = "plain";
        say("334 ");
      } else if (verb === "QUIT") {
        say("221 2.0.0 Bye");
        socket.end();
      } else say("250 2.0.0 OK");
    }
  });
}).listen(465);
`,
);
const autoconfig = container(
  "autoconfig",
  NODE,
  ["-v", `${settings}:/srv:ro`],
  ["node", "/srv/serve.mjs"],
);

const address = (id) =>
  docker(
    "inspect",
    "-f",
    "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}",
    id,
  );
const subnet = docker(
  "network",
  "inspect",
  name,
  "-f",
  "{{range .IPAM.Config}}{{.Subnet}}{{end}}",
);

// --- emguio ----------------------------------------------------------------------------------

step("starting emguio");
const port = await free();
const BASE = `http://127.0.0.1:${port}`;
const data = join(work, "data");
mkdirSync(data);
const emguio = container(
  "emguio",
  ALPINE,
  [
    "--user",
    `${process.getuid()}:${process.getgid()}`,
    // Nothing outside resolves: example.com is these containers, and nobody else's settings are
    // looked up instead.
    "--dns",
    "127.0.0.1",
    "--add-host",
    `imap.example.com:${address(mailserver)}`,
    "--add-host",
    `smtp.example.com:${address(autoconfig)}`,
    "--add-host",
    `autoconfig.example.com:${address(autoconfig)}`,
    "-p",
    `127.0.0.1:${port}:80`,
    "-v",
    `${app}:/app:ro`,
    "-v",
    `${data}:/data`,
    "-v",
    `${join(certs, "ca.pem")}:/ca.pem:ro`,
    "-w",
    "/app",
    "-e",
    `EMGUIO_PUBLIC_URL=${BASE}`,
    "-e",
    "EMGUIO_DATA_DIR=/data",
    "-e",
    `EMGUIO_SECRET_KEY=${randomBytes(32).toString("base64")}`,
    "-e",
    `EMGUIO_ALLOW_NETWORKS=${subnet}`,
    "-e",
    "SSL_CERT_FILE=/ca.pem",
  ],
  ["/app/emguio", "serve"],
);

// An empty database means nobody can sign in, so serve prints the one way in.
const logs = () =>
  spawnSync("docker", ["logs", emguio], { encoding: "utf8" }).output.join("");
const invite = await waitFor(
  () => /\/invite\/([A-Za-z0-9_-]+)/.exec(logs())?.[1],
).catch(() => die("emguio printed no invitation", logs()));

// --- the mail ---------------------------------------------------------------------------------

/** A connection to the mail server, one command at a time, for filling it. */
class Imap {
  #socket;
  #lines = [];
  #waiting = null;
  #buffer = "";
  #next = 1;

  static open(port, ca) {
    return new Promise((done, fail) => {
      const imap = new Imap();
      imap.#socket = connect({
        host: "127.0.0.1",
        port,
        servername: "imap.example.com",
        ca,
      });
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

  /** Sends a command and waits for its answer; literal is sent once the server asks for it. */
  async command(text, { literal, already = false } = {}) {
    const tag = `t${this.#next++}`;
    this.#socket.write(`${tag} ${text}\r\n`);
    for (;;) {
      const line = await this.#line();
      if (line.startsWith("+") && literal) {
        this.#socket.write(Buffer.concat([literal, Buffer.from("\r\n")]));
        continue;
      }
      if (!line.startsWith(`${tag} `)) continue;
      if (line.startsWith(`${tag} OK`)) return;
      if (already && line.includes("ALREADYEXISTS")) return;
      die(`the mail server refused ${text.split(" ")[0]}`, line);
    }
  }

  close() {
    this.#socket.end();
  }
}

step("filling the mailbox");
const imapPort = Number(docker("port", mailserver, "993").split(":").pop());
const imap = await waitFor(() =>
  Imap.open(imapPort, readFileSync(join(certs, "ca.pem"))),
).catch(() => die("the mail server did not answer", logs()));
await imap.command(`LOGIN ${quote(mail.email)} ${quote(mail.password)}`);
for (const folder of folders)
  await imap.command(`CREATE ${quote(folder)}`, { already: true });

const moment = Date.parse(now);
const at = (ago) => new Date(moment - seconds(ago) * 1000);
const keyed = Object.fromEntries(messages.map((m) => [m.key, m]));
// Oldest first: a list is in the order mail arrived, and real mail arrives in the order it is
// dated.
const arrival = messages.toSorted((a, b) => seconds(b.ago) - seconds(a.ago));
for (const m of arrival) {
  const flags = [
    ...(m.unread ? [] : ["\\Seen"]),
    ...(m.flagged ? ["\\Flagged"] : []),
    ...(m.folder === "Drafts" ? ["\\Draft"] : []),
  ];
  const raw = Buffer.from(message(m), "utf8");
  await imap.command(
    `APPEND ${quote(m.folder)} (${flags.join(" ")}) ${quote(internalDate(at(m.ago)))} {${raw.length}}`,
    { literal: raw },
  );
}
imap.close();

/** A port nothing on this machine is listening on, a moment ago. */
function free() {
  return new Promise((done) => {
    const probe = createServer().listen(0, "127.0.0.1", () => {
      const { port } = probe.address();
      probe.close(() => done(port));
    });
  });
}

function seconds(span) {
  const [, n, unit] =
    /^(\d+)([mhdw])$/.exec(span) ?? die(`not a span: ${span}`);
  return Number(n) * { m: 60, h: 3600, d: 86400, w: 604800 }[unit];
}

/** The message as the server stores it: headers, then its text, and its parts if it has any. */
function message(m) {
  const id = (key) => `<${key}@screenshots.example>`;
  const thread = [];
  for (let up = m.replyTo; up; up = keyed[up]?.replyTo) thread.unshift(id(up));
  const head = [
    `From: ${m.from}`,
    `To: ${m.to ?? `${mail.name} <${mail.email}>`}`,
    `Subject: ${m.subject}`,
    `Date: ${at(m.ago).toUTCString()}`,
    `Message-ID: ${id(m.key)}`,
    ...(thread.length
      ? [`In-Reply-To: ${thread.at(-1)}`, `References: ${thread.join(" ")}`]
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
  return Buffer.alloc(size, "emguio screenshots ");
}

/** A time as APPEND takes it: 17-Jun-2026 10:05:00 +0000. */
function internalDate(d) {
  const months = "Jan Feb Mar Apr May Jun Jul Aug Sep Oct Nov Dec".split(" ");
  const two = (n) => String(n).padStart(2, "0");
  return `${two(d.getUTCDate())}-${months[d.getUTCMonth()]}-${d.getUTCFullYear()} ${two(d.getUTCHours())}:${two(d.getUTCMinutes())}:${two(d.getUTCSeconds())} +0000`;
}

function quote(s) {
  return `"${s.replace(/[\\"]/g, "\\$&")}"`;
}

// --- the browser -----------------------------------------------------------------------------

browser = await Browser.launch({
  chromium,
  // Not a fixed one: another browser already on it would be driven instead, and its own work
  // would push this page to the back.
  port: await free(),
  profile: join(work, "chrome"),
  base: BASE,
});
await browser.scheme(THEME);
await browser.send("Emulation.setTimezoneOverride", { timezoneId: "UTC" });
await browser.send("Emulation.setLocaleOverride", { locale: "en-US" });
// The page's clock starts at the seed's moment, so the dates it draws are the same on every run.
// The server's own times are now, which is later: a time ahead of the page reads "just now".
await browser.send("Page.addScriptToEvaluateOnNewDocument", {
  source: `(() => {
    const Real = Date;
    const shift = ${moment} - Real.now();
    globalThis.Date = class extends Real {
      constructor(...args) {
        if (args.length === 0) super(Real.now() + shift);
        else super(...args);
      }
      static now() {
        return Real.now() + shift;
      }
    };
  })()`,
});
// Without the Headless, which is what a list of browsers would otherwise name.
const agent = (await browser.eval("navigator.userAgent")).replace(
  "HeadlessChrome",
  "Chrome",
);
await browser.send("Emulation.setUserAgentOverride", { userAgent: agent });

/** One call as the signed-in user. Any failure ends the run. */
async function api(path) {
  const { cookies } = await browser.send("Network.getCookies", {
    urls: [BASE],
  });
  const cookie = cookies.find((c) => c.name === "emguio_auth")?.value;
  if (!cookie) die("not signed in");
  const res = await fetch(BASE + path, {
    headers: { Cookie: `emguio_auth=${cookie}` },
  });
  const text = await res.text();
  if (!res.ok) die(`GET ${path} answered ${res.status}`, text);
  return JSON.parse(text);
}

// --- the shots -----------------------------------------------------------------------------

/** What the shot under way is waiting for, which is what it says when it runs out of time. */
let awaiting = "";

/** Polls f until it holds, saying what for. The shot's own time limit is what ends it. */
function until(what, f) {
  awaiting = what;
  return waitFor(f, TIMEOUT * 4);
}

/** Everything drawn: fonts in, pictures decoded, and the dialog's entrance over. */
async function settled() {
  await until("the fonts", () =>
    browser.eval("document.fonts.ready.then(() => true)"),
  );
  await until("the pictures to decode", () =>
    browser.eval(
      "[...document.images].every((i) => i.complete && i.naturalWidth > 0)",
    ),
  );
  await sleep(400);
}

const click = (selector, text) =>
  browser.eval(`(() => {
    const el = [...document.querySelectorAll(${JSON.stringify(selector)})].find((e) => e.textContent.trim() === ${JSON.stringify(text)});
    if (!el) throw new Error("nothing to click: " + ${JSON.stringify(text)});
    el.click();
    return true;
  })()`);

/** The field a label names, as an expression, inside scope. */
const field = (label, scope = "document") =>
  `[...${scope}.querySelectorAll("label")].find((l) => l.querySelector("span")?.textContent.trim() === ${JSON.stringify(label)})?.querySelector("input, select, textarea")`;

/** Typed, as a person would, into the field: React hears it as input. */
async function type(expression, text) {
  await browser.eval(`(${expression}.focus(), true)`);
  await browser.send("Input.insertText", { text });
}

/** The region an element takes with its notes, and a margin, inside the window. */
const around = (selector, margin = 16) =>
  browser.eval(`(() => {
    const rects = [document.querySelector(${JSON.stringify(selector)}), ...document.querySelectorAll("[data-note]")]
      .map((el) => el.getBoundingClientRect());
    const x = Math.max(0, Math.min(...rects.map((r) => r.left)) - ${margin});
    const y = Math.max(0, Math.min(...rects.map((r) => r.top)) - ${margin});
    const right = Math.min(innerWidth, Math.max(...rects.map((r) => r.right)) + ${margin});
    const bottom = Math.min(innerHeight, Math.max(...rects.map((r) => r.bottom)) + ${margin});
    return { x, y, width: right - x, height: bottom - y };
  })()`);

/**
 * Popovers over the page, each pointing at what it explains.
 *
 * A note is { at, text?, tight?, say, side, dx?, dy?, width? }: at is a selector, and text picks
 * the one whose words include it. The popover goes on that side of it, nudged by dx and dy, with
 * an arrow on the edge facing it; side "over" sits on it without one. Shown as a popover, because
 * a dialog is in the top layer and anything else is drawn under it.
 */
const annotate = (notes) =>
  browser.eval(`(() => {
    const notes = ${JSON.stringify(notes)};
    const dark = matchMedia("(prefers-color-scheme: dark)").matches;
    const ground = dark ? "#f4f4f5" : "#1d1d20", ink = dark ? "#18181b" : "#fafafa";
    const host = document.createElement("div");
    host.popover = "manual";
    host.style.cssText = "position:fixed;inset:0;width:100vw;height:100vh;margin:0;padding:0;border:0;background:none;overflow:visible;pointer-events:none";
    document.body.append(host);
    host.showPopover();
    for (const n of notes) {
      const target = [...document.querySelectorAll(n.at)].find((el) => !n.text || el.textContent.includes(n.text));
      if (!target) throw new Error("no " + n.at + (n.text ? " saying " + n.text : ""));
      // tight measures the words rather than the box, for a line whose box is the whole width:
      // the text that says them, or the element's first.
      let r = target.getBoundingClientRect();
      if (n.tight) {
        const walk = document.createTreeWalker(target, NodeFilter.SHOW_TEXT);
        let node;
        while ((node = walk.nextNode()) && !(node.textContent.trim() && (!n.text || node.textContent.includes(n.text))));
        const range = document.createRange();
        range.selectNodeContents(node ?? target);
        r = range.getBoundingClientRect();
      }
      const box = document.createElement("div");
      box.dataset.note = "";
      box.textContent = n.say;
      box.style.cssText = "position:absolute;max-width:" + (n.width ?? 240) + "px;padding:7px 11px;border-radius:9px;" +
        "background:" + ground + ";color:" + ink + ";font:500 13px/1.35 system-ui,sans-serif;" +
        "box-shadow:0 1px 2px rgb(0 0 0 / 0.12),0 8px 24px rgb(0 0 0 / 0.18)";
      host.append(box);
      const b = box.getBoundingClientRect();
      const gap = 10;
      let x = { right: r.right + gap, left: r.left - gap - b.width }[n.side] ?? r.left + r.width / 2 - b.width / 2;
      let y = { below: r.bottom + gap, above: r.top - gap - b.height }[n.side] ?? r.top + r.height / 2 - b.height / 2;
      x = Math.min(innerWidth - b.width - 10, Math.max(10, x + (n.dx ?? 0)));
      y = Math.min(innerHeight - b.height - 10, Math.max(10, y + (n.dy ?? 0)));
      box.style.left = x + "px";
      box.style.top = y + "px";
      if (n.side === "over") continue;
      // A square turned half over, on the edge that faces the target and level with its middle.
      const arrow = document.createElement("div");
      const along = (from, size, mid) => Math.min(size - 14, Math.max(14, mid - from));
      const horizontal = n.side === "right" || n.side === "left";
      const at = horizontal ? along(y, b.height, r.top + r.height / 2) : along(x, b.width, r.left + r.width / 2);
      arrow.style.cssText = "position:absolute;width:10px;height:10px;background:" + ground + ";transform:rotate(45deg);" +
        (horizontal ? "top:" + (at - 5) + "px;" + (n.side === "right" ? "left:-4px" : "right:-4px")
                    : "left:" + (at - 5) + "px;" + (n.side === "below" ? "top:-4px" : "bottom:-4px"));
      box.append(arrow);
    }
    return true;
  })()`);

/**
 * Everything at rest: the caret hidden and every transition and animation finished, which would
 * otherwise differ from run to run.
 */
const still = () =>
  browser.eval(`(() => {
    const style = document.createElement("style");
    style.textContent =
      "*, ::before, ::after { caret-color: transparent !important; transition: none !important; }";
    document.head.append(style);
    for (const animation of document.getAnimations()) animation.finish();
    return new Promise((done) => requestAnimationFrame(() => requestAnimationFrame(() => done(true))));
  })()`);

// The install, in order: each shot brings the page to a step and is taken there; then completes
// it, whether or not it was taken, since the next one starts where it left off. A shot is the
// whole window unless it names an element, which is photographed alone.
const shots = [
  {
    name: "invite",
    window: [640, 420],
    go: async () => {
      await browser.open(`/invite/${invite}`);
      await until("the invitation's form", () =>
        browser.eval(`!!document.querySelector("input[name=username]")`),
      );
      await type(
        `document.querySelector("input[name=username]")`,
        account.username,
      );
      await type(
        `document.querySelector("input[name=password]")`,
        account.password,
      );
    },
    then: async () => {
      await click("button", "Create it");
      await until("the user to be made", () =>
        browser.eval(
          `location.pathname === "/" && !!document.querySelector("header")`,
        ),
      );
    },
  },
  {
    name: "account",
    window: [1000, 1000],
    element: "dialog[open]",
    go: async () => {
      await browser.open("/settings/email-configs/new");
      const dialog = `document.querySelector("dialog[open]")`;
      await until("the account form", () =>
        browser.eval(`!!${field("Email address", dialog)}`),
      );
      await type(field("Email address", dialog), mail.email);
      // Leaving the address is what looks its settings up, and filling them in takes over the
      // fields: typed into after.
      await browser.eval(`(${field("Name", dialog)}.focus(), true)`);
      await until("the settings to be found", () =>
        browser.eval(`${field("Host", dialog)}?.value === "imap.example.com"`),
      );
      await type(field("Password", dialog), mail.password);
      await type(field("Your name", dialog), mail.name);
      await click("dialog[open] button", "Test connection");
      const results = `[...document.querySelectorAll("dialog[open] li")].map((li) => li.textContent)`;
      await until("the test to sign in to both servers", async () => {
        const said = await browser.eval(results);
        awaiting = `the test to sign in to both servers; it says: ${said.join(" ") || "nothing yet"}`;
        return said.filter((line) => line.endsWith("signed in.")).length === 2;
      });
    },
    then: async () => {
      await click("dialog[open] button", "Save");
      await until("the account to be saved", () =>
        browser.eval(`!document.querySelector("dialog[open]")`),
      );
    },
  },
  {
    name: "mail",
    window: [1280, 800],
    go: async () => {
      // Kept once the first look is over: every folder listed, and Inbox's newest in the window.
      const config = await until(
        "the account",
        async () => (await api("/api/email-configs")).email_configs[0],
      );
      const inbox = await until("the first look at the mailbox", async () =>
        (await api(`/api/email-configs/${config.id}/mailboxes`)).mailboxes.find(
          (mb) => mb.special_use === "inbox" && mb.messages > 0,
        ),
      );
      const subject = keyed[opened].subject;
      const open = await until(
        "the message to open in Inbox's list",
        async () =>
          (
            await api(
              `/api/email-configs/${config.id}/mailboxes/${inbox.id}/messages`,
            )
          ).messages.find((m) => m.subject === subject && !m.seen),
      );
      await browser.open(`/c/${config.id}/${inbox.id}/${open.id}`);
      const first = keyed[opened].body.split("\n")[0];
      await until("the message to be read", () =>
        browser.eval(
          `[...document.querySelectorAll("article")].some((a) => a.textContent.includes(${JSON.stringify(first)}))`,
        ),
      );
      // The counts beside the rows, which arrive after them.
      await until("the conversation counts", () =>
        browser.eval(
          `[...document.querySelectorAll("ul[aria-label=Messages] li")].some((li) => li.textContent.includes("Conversation of"))`,
        ),
      );
    },
  },
];

step(`photographing at ${SCALE}x, ${THEME}`);
for (const shot of shots) {
  // A step waiting on something that never comes would otherwise wait forever.
  awaiting = "";
  const limit = setTimeout(
    () =>
      die(
        `${shot.name} took more than ${TIMEOUT} seconds${awaiting ? `, waiting for ${awaiting}` : ""}`,
      ),
    TIMEOUT * 1000,
  );
  const taken = !ONLY.length || ONLY.includes(shot.name);
  await browser.viewport(...shot.window, SCALE);
  await shot.go();
  if (taken) {
    await settled();
    awaiting = "the page to come to rest";
    await still();
    if (NOTES && shot.notes) await annotate(shot.notes);
    const clip = shot.element ? await around(shot.element) : undefined;
    const file = join(OUT, `${shot.name}.png`);
    const png = stamp(await browser.png(clip), SCALE);
    const before = existsSync(file) ? readFileSync(file) : null;
    if (before && alike(before, png)) {
      // The same picture, kept; its metadata brought up to date if the stamp has changed.
      const restamped = stamp(before, SCALE);
      if (!restamped.equals(before)) writeFileSync(file, restamped);
      console.log(`  ${shot.name}.png, unchanged`);
    } else {
      writeFileSync(file, png);
      console.log(`  ${shot.name}.png`);
    }
  }
  await shot.then?.();
  clearTimeout(limit);
}

step("done");
process.exit(0);

/**
 * The density a PNG was drawn at, said the two ways a macOS screenshot says it: a pHYs chunk,
 * and an EXIF block with the same resolution in inches, which is what some of the system reads
 * instead. A 2x capture says 144 dpi, and a viewer that reads it shows it at the size it was on
 * screen rather than twice that.
 */
function stamp(png, scale) {
  const chunk = (type, data) => {
    const head = Buffer.alloc(4);
    head.writeUInt32BE(data.length);
    const body = Buffer.concat([Buffer.from(type), data]);
    const tail = Buffer.alloc(4);
    tail.writeUInt32BE(crc32(body));
    return Buffer.concat([head, body, tail]);
  };
  const dpi = 72 * scale;
  const phys = Buffer.alloc(9);
  phys.writeUInt32BE(Math.round(dpi / 0.0254), 0);
  phys.writeUInt32BE(Math.round(dpi / 0.0254), 4);
  phys[8] = 1; // the unit is the metre

  // A big-endian TIFF header and one directory of three entries: XResolution and YResolution,
  // each a rational stored after the directory, and ResolutionUnit, 2 being the inch.
  const exif = Buffer.alloc(66);
  exif.write("MM", 0, "latin1");
  exif.writeUInt16BE(42, 2);
  exif.writeUInt32BE(8, 4);
  exif.writeUInt16BE(3, 8);
  const entry = (at, tag, type, value) => {
    exif.writeUInt16BE(tag, at);
    exif.writeUInt16BE(type, at + 2);
    exif.writeUInt32BE(1, at + 4);
    if (type === 3) exif.writeUInt16BE(value, at + 8);
    else exif.writeUInt32BE(value, at + 8);
  };
  entry(10, 0x011a, 5, 50);
  entry(22, 0x011b, 5, 58);
  entry(34, 0x0128, 3, 2);
  exif.writeUInt32BE(0, 46); // no next directory
  for (const at of [50, 58]) {
    exif.writeUInt32BE(dpi, at);
    exif.writeUInt32BE(1, at + 4);
  }

  // After IHDR, which is always first, and in place of any already there.
  const parts = [png.subarray(0, 8)];
  for (let at = 8; at < png.length;) {
    const length = png.readUInt32BE(at);
    const type = png.toString("latin1", at + 4, at + 8);
    const end = at + 12 + length;
    if (type !== "pHYs" && type !== "eXIf") parts.push(png.subarray(at, end));
    if (type === "IHDR") parts.push(chunk("pHYs", phys), chunk("eXIf", exif));
    at = end;
  }
  return Buffer.concat(parts);
}

/**
 * Whether two captures are one picture, give or take the rasteriser.
 *
 * With the content pinned, a run still moved a few dozen anti-aliased pixels at an edge by a few
 * levels now and then, and git saw a new file. Kept when under a thousandth of the pixels differ
 * and none by more than 24 of 255: a moved icon or a changed word is far more than 24 levels,
 * and a changed colour is far more than a thousandth of the picture.
 */
function alike(before, after) {
  const a = decode(before);
  const b = decode(after);
  if (
    !a ||
    !b ||
    a.width !== b.width ||
    a.height !== b.height ||
    a.channels !== b.channels
  ) {
    return false;
  }
  let differing = 0;
  for (let at = 0; at < a.pixels.length; at += a.channels) {
    let worst = 0;
    for (let c = 0; c < a.channels; c++) {
      worst = Math.max(worst, Math.abs(a.pixels[at + c] - b.pixels[at + c]));
    }
    if (worst > 24) return false;
    if (worst > 0) differing++;
  }
  return differing <= (a.pixels.length / a.channels) * 0.001;
}

/** The pixels of an 8-bit, non-interlaced RGB or RGBA PNG, which is what Chromium writes. */
function decode(png) {
  let width, height, channels;
  const data = [];
  for (let at = 8; at < png.length;) {
    const length = png.readUInt32BE(at);
    const type = png.toString("latin1", at + 4, at + 8);
    const body = png.subarray(at + 8, at + 8 + length);
    if (type === "IHDR") {
      width = body.readUInt32BE(0);
      height = body.readUInt32BE(4);
      channels = { 2: 3, 6: 4 }[body[9]];
      if (body[8] !== 8 || !channels || body[12] !== 0) return null;
    }
    if (type === "IDAT") data.push(body);
    at += 12 + length;
  }
  const raw = inflateSync(Buffer.concat(data));
  const stride = width * channels;
  const pixels = Buffer.alloc(height * stride);
  for (let y = 0; y < height; y++) {
    const filter = raw[y * (stride + 1)];
    const line = y * (stride + 1) + 1;
    for (let x = 0; x < stride; x++) {
      const a = x >= channels ? pixels[y * stride + x - channels] : 0;
      const b = y > 0 ? pixels[(y - 1) * stride + x] : 0;
      const c =
        x >= channels && y > 0 ? pixels[(y - 1) * stride + x - channels] : 0;
      let value = raw[line + x];
      if (filter === 1) value += a;
      else if (filter === 2) value += b;
      else if (filter === 3) value += (a + b) >> 1;
      else if (filter === 4) {
        const p = a + b - c;
        const pa = Math.abs(p - a),
          pb = Math.abs(p - b),
          pc = Math.abs(p - c);
        value += pa <= pb && pa <= pc ? a : pb <= pc ? b : c;
      }
      pixels[y * stride + x] = value & 255;
    }
  }
  return { width, height, channels, pixels };
}
