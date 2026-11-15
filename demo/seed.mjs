// Fills the demo: every account's mail on the mail server, then emguio's user, signed up through
// the invitation emguio printed, and the five accounts added to it. Once per start: a seed that
// finds the invitation spent leaves what is there alone.
import { readFileSync } from "node:fs";
import { setTimeout as sleep } from "node:timers/promises";

import { fill, Imap } from "../web/scripts/mail.mjs";
import { accounts, owner, password, user } from "./accounts.mjs";

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
const link = readFileSync("/handoff/invite", "utf8").trim();
const token = link.split("/invite/")[1];
if ((await fetch(`${EMGUIO}/api/auth/invites/${token}`)).status === 404) {
  console.log(
    "already set up: sign in at http://localhost:8080 as demo / demodemo",
  );
  process.exit(0);
}

for (const account of accounts) {
  const imap = await until("the mail server", () =>
    Imap.open({
      host: "imap.example.com",
      port: 993,
      servername: "imap.example.com",
      ca,
    }),
  );
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

const joined = await fetch(`${EMGUIO}/api/auth/invites/${token}/accept`, {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(user),
});
if (!joined.ok)
  throw new Error(
    `signing up answered ${joined.status}: ${await joined.text()}`,
  );
const cookie = joined.headers
  .getSetCookie()
  .map((c) => c.split(";")[0])
  .find((c) => c.startsWith("emguio_auth="));

for (const account of accounts) {
  const server = (host, port) => ({
    host,
    port,
    tls: "implicit",
    username: account.email,
    password,
  });
  const res = await fetch(`${EMGUIO}/api/email-configs`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Cookie: cookie },
    body: JSON.stringify({
      name: account.name,
      email: account.email,
      sender_name: owner,
      incoming: { protocol: "imap", ...server("imap.example.com", 993) },
      outgoing: server("smtp.example.com", 465),
    }),
  });
  if (!res.ok)
    throw new Error(
      `adding ${account.email} answered ${res.status}: ${await res.text()}`,
    );
}
console.log("ready: sign in at http://localhost:8080 as demo / demodemo");
