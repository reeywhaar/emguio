// Fills the demo: every account's mail on the mail server, which starts empty, then emguio's
// user, signed up through the invitation emguio printed or kept from an earlier run, and the five
// accounts added to it. Once per start: a seed that finds it all there leaves it alone.
import { readFileSync } from "node:fs";
import { setTimeout as sleep } from "node:timers/promises";

import { fill, Imap, quote } from "../web/scripts/mail.mjs";
import { accounts, owner, password, sealKey, user } from "./accounts.mjs";

const EMGUIO = "http://emguio";
const ca = readFileSync("/certs/ca.pem");

/** Waits for what answers, up to two minutes, saying what it waited for if it never does. */
async function until(what, ask) {
  const deadline = Date.now() + 120_000;
  for (;;) {
    try {
      const answer = await ask();
      if (answer) return answer;
    } catch (err) {
      if (Date.now() > deadline)
        throw new Error(`${what} never answered: ${err.message}`);
    }
    if (Date.now() > deadline) throw new Error(`${what} never answered`);
    await sleep(1000);
  }
}

await until("emguio", async () => (await fetch(`${EMGUIO}/healthz`)).ok);

/** The session cookie a sign-in or a sign-up answered with. */
const cookieOf = (res) =>
  res.headers
    .getSetCookie()
    .map((c) => c.split(";")[0])
    .find((c) => c.startsWith("emguio_auth="));
const json = (body) => ({
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(body),
});

// The user signed up on an earlier run is kept, and the browser's session with it.
const signedIn = await fetch(`${EMGUIO}/api/auth/login`, json(user));
let cookie = signedIn.ok ? cookieOf(signedIn) : undefined;

/** What emguio answers the demo user, signed in. */
const as = (path, method = "GET", body) =>
  fetch(`${EMGUIO}${path}`, {
    method,
    headers: {
      Cookie: cookie,
      ...(body === undefined ? {} : { "Content-Type": "application/json" }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });

// emguio starts sealed: the kept user's seal key is opened, or made for one kept from before
// seal keys, whose accounts' passwords are then saved again.
let resave = false;
if (cookie) {
  const me = await (await as("/api/auth/me")).json();
  const opening =
    me.seal === "sealed"
      ? await as("/api/seal/unseal", "POST", { key: sealKey })
      : me.seal === "none"
        ? await as("/api/seal", "POST", { key: sealKey })
        : undefined;
  if (opening && !opening.ok)
    throw new Error(
      `opening the seal key answered ${opening.status}: ${await opening.text()}`,
    );
  resave = me.seal === "none";
}

// The mail server starts empty: mail in it is this start's, put in by a seed before this one.
const open = () =>
  until("the mail server", () =>
    Imap.open({
      host: "imap.example.com",
      port: 993,
      servername: "imap.example.com",
      ca,
    }),
  );
const peek = await open();
await peek.command(`LOGIN ${quote(accounts[0].email)} ${quote(password)}`);
const filled = (await peek.count("INBOX")) > 0;
peek.close();
if (filled && cookie) {
  console.log(
    "already set up: sign in at http://localhost:8080 as demo / demodemo",
  );
  process.exit(0);
}

if (!filled) {
  for (const account of accounts) {
    const imap = await open();
    await fill(
      imap,
      { email: account.email, name: owner, password },
      {
        folders: account.folders,
        messages: account.messages,
        now: Date.now(),
        domain: "demo.example",
      },
    );
    imap.close();
    console.log(`${account.email}: ${account.messages.length} messages`);
  }
}

if (!cookie) {
  const link = readFileSync("/handoff/invite", "utf8").trim();
  const token = link.split("/invite/")[1];
  const joined = await fetch(
    `${EMGUIO}/api/auth/invites/${token}/accept`,
    json({ ...user, seal_key: sealKey }),
  );
  if (!joined.ok)
    throw new Error(
      `signing up answered ${joined.status}: ${await joined.text()}`,
    );
  cookie = cookieOf(joined);
}

// Accounts kept from an earlier run stay, and are read again for this start's mail: emguio read
// them while the mail server was still empty. One not there yet is added.
const listed = await as("/api/email-configs");
const kept = (await listed.json()).email_configs;
for (const account of accounts) {
  const server = (host, port) => ({
    host,
    port,
    tls: "implicit",
    username: account.email,
    password,
  });
  const body = {
    name: account.name,
    email: account.email,
    sender_name: owner,
    incoming: { protocol: "imap", ...server("imap.example.com", 993) },
    outgoing: server("smtp.example.com", 465),
  };
  const there = kept.find((c) => c.email === account.email);
  if (there && !resave) {
    await as(`/api/email-configs/${there.id}/sync`, "POST");
    continue;
  }
  const res = there
    ? await as(`/api/email-configs/${there.id}`, "PUT", body)
    : await as("/api/email-configs", "POST", body);
  if (!res.ok)
    throw new Error(
      `saving ${account.email} answered ${res.status}: ${await res.text()}`,
    );
}
console.log("ready: sign in at http://localhost:8080 as demo / demodemo");
