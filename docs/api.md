# emguio API

emguio is a self-hosted webmail. Each user adds their mail accounts — an IMAP server to read
from and, optionally, an SMTP server to send through — and reads, files and writes mail through
emguio. The web interface does all of it through the HTTP API described here, and a program can
do the same.

This page is the whole reference: signing in, the shape of requests and answers, every endpoint
and every error. Paths are relative to the instance's address. The examples use
`https://emguio.example.com`, and `$EC`, `$MB`, `$MSG` and the like for ids taken from earlier
answers.

## Authenticating

There are no API tokens. A program signs in as a user, with that user's username and password,
and keeps the cookie it is given, as a browser does.

```
POST /api/auth/login
```

The body is `{"username": "…", "password": "…"}`. The username is matched without regard to
case. A match answers `204` with no body and sets the cookie `emguio_auth`. A wrong username and
a wrong password both answer `401 unauthenticated`, alike.

```sh
curl -c jar -b jar -H 'Content-Type: application/json' \
  -d '{"username":"ann","password":"correct horse battery staple"}' \
  https://emguio.example.com/api/auth/login

curl -c jar -b jar https://emguio.example.com/api/auth/me
```

`-b jar` sends the cookie kept in the file `jar`, and `-c jar` keeps whatever cookie comes back.
Pass both on every call: the cookie is set again as its window slides. `curl -d` sends
`application/x-www-form-urlencoded` unless told otherwise, so the `Content-Type` header is needed.

The cookie is `HttpOnly`, `SameSite=Lax` and `Path=/`, and `Secure` when the instance is served
over https. It lasts seven days from when it was last used. A request that comes more than an
hour after the window last moved pushes the expiry seven days on and sets the cookie again. A
session nobody uses for seven days is over; `POST /api/auth/logout` ends one at once.

Every endpoint but sign-in and the two invitation endpoints needs a live session. Without one the
answer is `401` with code `unauthenticated` — never a redirect. A cookie naming a session that is
over is cleared in that same answer. Sign in again and repeat the request.

Sign-in attempts are limited, in two buckets. Past either, the answer is `429 rate_limited`.

| bucket | burst | then |
| --- | --- | --- |
| every sign-in to the instance | 30 | one every 2 seconds |
| each username, without regard to case | 5 | one every 20 seconds |

Every attempt spends from both, whether or not the password matches. A body that is not the JSON
expected is refused before either is spent.

## Requests and responses

**JSON in, JSON out.** A request body is one JSON object, sent as `application/json`; a
`charset` parameter is fine. Every answer with a body is JSON, except a message part, a proxied
image and the event stream, each described where it occurs.

**Unknown fields are refused.** A body field this page does not list for that endpoint answers
`400 invalid`, naming the field, rather than being ignored. A field left out is its zero value:
`""`, `0`, `false`, or `null` for an object or a list.

**Bodies have a cap.** 1 MB, except sending and drafts, which take up to 36 MB so attachments
fit. A larger body answers `413 body_too_large`.

**Requests from other sites are refused.** A request with any method but `GET`, `HEAD` and
`OPTIONS` answers `403 forbidden` when it carries `Sec-Fetch-Site: cross-site`, and
`415 unsupported_media_type` when it has a body that is not `application/json`. A browser sets
`Sec-Fetch-Site` and a page cannot forge it; a program that sends no `Sec-Fetch-Site` at all is
fine. A request with no body, like a `DELETE`, needs no `Content-Type`. emguio sends no CORS
headers, so a page on another origin cannot read its answers.

**Success has no envelope.** A successful answer is the object described for its endpoint, or
`204` with no body. A refusal is always `{"ok": false, "code": "…", "message": "…"}`; see
[Errors](#errors). A program that reads bodies without their status line tells them apart by
`"ok": false`.

**Ids** are a prefix and 26 characters of lowercase Crockford base32 (digits and letters, without
`i`, `l`, `o` and `u`). They are opaque: keep them and send them back as given. A malformed one
answers `400 invalid` rather than `404`.

| prefix | names |
| --- | --- |
| `u_` | a user |
| `ec_` | an email config: one of the user's mail accounts |
| `mb_` | a mailbox: a folder on the incoming server |
| `j_` | a job: an action on a message |
| `d_` | a draft kept in emguio |
| `dp_` | an attachment a draft holds |

**A message is named by what its server calls it.** Most messages have no row in emguio to name
them by, so a message's id is `{uidvalidity}-{uid}`: its folder's UIDVALIDITY and its UID, two
positive 32-bit numbers joined by a dash, like `1700000000-4521`. It is always used under its
folder's `mb_` id. A message moved to another folder has another id there. A server that
renumbers a folder changes its UIDVALIDITY, and every message id from before then answers
`404 gone` rather than naming whatever holds that UID now: list the folder again.

**Times** are Unix seconds, UTC, as JSON numbers. One that may be absent is `null`.

**Some calls wait for the mail server.** emguio keeps each mail account's folders, with their
counts, and the headers of INBOX's newest 30 messages — nothing else. Any other folder's list, an
older page of INBOX, a search, a message, a part, a conversation and sending are each asked of
the mail server while the request waits. They take as long as the server does, and can answer
`502 unreachable` (the server could not be reached, or refused, with its reason in `message`) or
`404 gone` (what was named is no longer there as it was named).

**Writes have a deadline.** A request with any method but `GET`, `HEAD` and `OPTIONS` is stopped
after two minutes, and one stopped while it waited answers `503 busy`. Try it again.

An unknown path under `/api/`, or a method a path does not take, answers `404 not_found`.
`/healthz` answers `{"ok": true, "version": "…"}` to anybody, for a liveness check.

## Mail accounts

The API calls a mail account an email config. Each has an incoming IMAP server and, optionally,
an outgoing SMTP server. Each stands alone: everything about its folders and mail is under
`/api/email-configs/{id}`, and nothing joins across them. Another user's id answers `404`, the
same as one that does not exist.

Once one is saved, emguio signs in to its incoming server in the background, lists its folders,
keeps INBOX up to date as mail arrives, and reads every folder's counts at least every five
minutes.

An email config:

```json
{"id": "ec_01k6r2n8v4b7c9d3f5g1h6j8k2", "name": "Work", "email": "ann@example.com", "sender_name": "Ann Smith",
 "incoming": {"protocol": "imap", "host": "imap.example.com", "port": 993, "tls": "implicit", "username": "ann@example.com"},
 "outgoing": {"host": "smtp.example.com", "port": 465, "tls": "implicit", "username": ""},
 "created_at": 1790000000, "updated_at": 1790000000, "synced_at": 1790000300, "sync_error": "", "inbox_unseen": 3,
 "archive_mailbox": null}
```

| field | |
| --- | --- |
| `name` | A label for it. May be empty. |
| `email` | The address mail is sent from. |
| `sender_name` | The name sent beside the address. Empty sends the address alone. |
| `incoming` | The IMAP server. `protocol` is always `imap`. |
| `outgoing` | The SMTP server, or `null` for none, in which case it cannot send. An empty `username` signs in with the incoming server's username and password. |
| `tls` | `implicit`, TLS from the first byte (usually ports 993 and 465), or `starttls` (usually 143 and 587). There is no unencrypted choice. |
| `synced_at` | When its mail was last brought up to date; `null` before the first time. |
| `sync_error` | Why the latest try did not, in a sentence; empty when it did. |
| `inbox_unseen` | How many messages in INBOX are unread. |
| `archive_mailbox` | The folder chosen to archive to; `null` for the one the server names. See below. |

No answer ever carries a password.

```
GET /api/email-configs
```

`{"email_configs": [ … ]}`, oldest first.

```
POST /api/email-configs
PUT /api/email-configs/{id}
```

Saves a new email config, or replaces one. The body:

```json
{"name": "Work", "email": "ann@example.com", "sender_name": "Ann Smith",
 "incoming": {"protocol": "imap", "host": "imap.example.com", "port": 993, "tls": "implicit",
              "username": "ann@example.com", "password": "…"},
 "outgoing": {"host": "smtp.example.com", "port": 465, "tls": "implicit", "username": "", "password": ""}}
```

- `email` is required: at most 254 bytes, with an `@` neither first nor last, and no spaces or
  `<>,;"`.
- `name` is at most 64 characters and `sender_name` at most 128, neither with control characters.
- `incoming.protocol` must be `imap`, and `incoming.username` is required.
- Each server needs a `host` on its own — a name or an address, with no scheme, port or path, at
  most 253 bytes — a `port` from 1 to 65535 and a `tls`. A `username` is at most 320 bytes and a
  `password` at most 1024. Hosts are lowercased.
- `outgoing` has no `protocol`. `null`, or leaving it out, is no outgoing server.
- Saving a new one needs the incoming password, and the outgoing one when the outgoing server
  has a username of its own.

`PUT` replaces the whole email config, so send every field: one left out is cleared, and leaving
out `outgoing` removes the outgoing server. A password left empty keeps the one saved, but only
while the host it would go to is the host it was saved for; for another host it has to be given
again. Another incoming host or username is another mail store: its folders are dropped, with
their `mb_` ids, and read again, and `synced_at` is `null` until they are.

`POST` answers `201` with the new email config and `PUT` `200` with the saved one; either starts
reading its mail at once. A field that is wrong answers `400 invalid`, saying which.

```
DELETE /api/email-configs/{id}
```

Removes the email config and everything emguio keeps of it: folders, jobs, drafts. Nothing on
the mail server changes. `204`.

```
PUT /api/email-configs/{id}/archive
```

Chooses the folder archiving moves to: `{"mailbox": "mb_…"}`, one of the email config's own
folders that holds messages, or `{"mailbox": ""}` for the one the server names — its Archive, or
Gmail's All Mail. Only this changes: the config's connections go on as they are. A folder the
server later drops goes back to the server's. Answers `200` with the email config. A folder that
is not the config's answers `404 not_found`, a malformed id or one that only holds folders
`400 invalid`.

```
POST /api/email-configs/test
POST /api/email-configs/{id}/test
```

Signs in to the servers a body names, shaped as for saving, and saves nothing. The first is for
an email config not saved yet, so every password is required. The second fills each empty
password from the saved email config `{id}`, under the same rule about hosts. IMAP signs in,
opens INBOX read-only and signs out; SMTP greets, signs in and quits without sending. Both
servers are tried at once, each for at most fifteen seconds.

```json
{"incoming": {"ok": true, "message": ""}, "outgoing": {"ok": false, "message": "…"}}
```

A server that refuses is `"ok": false` with its reason, still in a `200`: the test ran, and that
is its answer. `outgoing` is `null` when the body has none. A body that is wrong in itself
answers `400 invalid`. Tests are limited per user to a burst of ten, then one every six seconds;
past that, `429 rate_limited`.

```
POST /api/email-configs/autoconfig
```

Looks up the servers of an address's domain, for filling in a new email config: the body is
`{"email": "…"}`. Where it looks, most trusted first: the provider's own settings at
`https://autoconfig.{domain}/mail/config-v1.1.xml` or
`https://{domain}/.well-known/autoconfig/mail/config-v1.1.xml`; Mozilla's list of providers;
the domain's DNS SRV records; and Mozilla's list for the provider the domain's MX names. Only
IMAP and SMTP over TLS, signed in to with a password, count. The lookup takes at most eight
seconds.

```json
{"found": true, "source": "mozilla", "domain": "example.com",
 "incoming": {"protocol": "imap", "host": "imap.example.com", "port": 993, "tls": "implicit", "username": "ann@example.com"},
 "outgoing": {"host": "smtp.example.com", "port": 465, "tls": "implicit", "username": "ann@example.com"}}
```

`source` is `provider`, `mozilla` or `dns`, and `domain` the domain the settings are for —
another than the address's when they were found by its MX. `outgoing` is `null` when no
outgoing server was found. Nothing found is `200 {"found": false}`, with `"oauth_only": true`
when what was found signs in only with OAuth, which emguio does not do. What is not an address
answers `400 invalid`. Lookups are limited per user like tests: a burst of ten, then one every
six seconds; past that, `429 rate_limited`. Nothing is saved, and what is found is a guess to
check, with a test, before saving.

```
POST /api/email-configs/{id}/sync
```

Asks for every folder to be looked at now, and answers `202` with no body at once. What the look
finds shows in the folders, INBOX's list, `synced_at` and `sync_error`, and on the event stream.

## Folders and messages

Reading INBOX is: list the folders, take the one whose `special_use` is `inbox`, list its
messages, and read one by its id.

```
GET /api/email-configs/{id}/mailboxes
```

`{"mailboxes": [ … ]}`, read from what emguio keeps, so it answers at once. Empty until the first
look after the email config is saved. In the order a sidebar shows them: INBOX, Drafts, Sent,
Archive, All Mail, Flagged, Junk, Trash, each followed by the folders inside it, then the rest by
path.

```json
{"id": "mb_01k6r2p3q5s7t9v1w3x5y7z9a1", "name": "INBOX", "path": ["INBOX"], "special_use": "inbox",
 "selectable": true, "messages": 1204, "unseen": 3, "uid_next": 4522}
```

| field | |
| --- | --- |
| `name` | The server's name for it. |
| `path` | `name` split at the server's delimiter: where it sits in the tree. |
| `special_use` | What it is for: `inbox`, `drafts`, `sent`, `archive`, `all`, `flagged`, `junk`, `trash`, or empty. From the server's SPECIAL-USE flags, or else from a usual name like "Sent Items". |
| `selectable` | `false` for one that only holds other folders and has no messages of its own. |
| `messages`, `unseen` | How many messages it holds and how many are unread, at the last look. |
| `uid_next` | Grows with every message put in it, so a change shows even when the counts end up the same. |

A folder's id lasts while the server keeps its name. One renamed on the server is a new folder
with a new id.

```
POST /api/email-configs/{id}/mailboxes
```

Makes a folder on the mail server: `{"name": "Projects"}` at the top of the user's own folders,
or with `"parent": "mb_…"` inside that one. At the top means under the server's personal
namespace, `INBOX.` on servers that keep everything under INBOX. The folders are then listed
again, and the answer is `201` with the new one, shaped as above, ready to move messages into.

A name is one line of at most 200 characters, without the server's delimiter: nesting is what
`parent` is for. An empty or wrong name answers `400 invalid`; one a sibling has already,
compared without regard to case, `409 conflict`. A server that refuses answers
`502 unreachable` with its reason.

```
GET /api/email-configs/{id}/mailboxes/{mailbox}/messages
```

A page of a folder's messages, newest to arrive first. Query parameters:

| parameter | |
| --- | --- |
| `limit` | How many, from 1 to 200; 50 by default. |
| `cursor` | The `next_cursor` of the page before, for the page after it. |
| `q` | A search, at most 500 bytes; see [Search](#search). |

```json
{"messages": [ … ], "next_cursor": "1700000000-4471"}
```

`next_cursor` is there only while older messages remain; pass it, with the same `q`, for the next
page. Pages after the first are read by UID, so mail arriving or leaving in between does not shift
them. A cursor from before the folder's UIDVALIDITY changed answers `404 gone`: start the list
again without one.

INBOX's first page, without `q`, is the 30 newest messages emguio keeps: it answers at once,
whatever `limit` says. Every other page, folder and search is read from the mail server.

A message as a list shows it:

```json
{"id": "1700000000-4521", "from": {"name": "Bob Jones", "email": "bob@example.org"},
 "to": [{"name": "", "email": "ann@example.com"}], "subject": "Lunch", "date": 1790000000,
 "sent": 1789999990, "seen": false, "flagged": false, "answered": false, "draft": false,
 "has_attachments": false, "preview": "Are you free on Thursday?"}
```

| field | |
| --- | --- |
| `subject` | Empty when it has none. |
| `date` | When it arrived at the server. The list is in this order. |
| `sent` | Its own `Date` header, which is the sender's claim; `null` when it has none. |
| `seen`, `flagged`, `answered`, `draft` | Its flags on the server: read, starred, replied to, a draft. |
| `preview` | The start of its text on one line, cut at 160 characters. May be empty. |

Listing a folder changes nothing on the server, and neither does reading a message. To mark one
read, post a `seen` job.

```
GET /api/email-configs/{id}/mailboxes/{mailbox}/messages/{message}
```

One message whole, without the bytes of its attachments, fetched from the mail server every time.
It is the list's message object, with these besides:

| field | |
| --- | --- |
| `mailbox` | Its folder's id. |
| `cc` | A list of addresses, like `to`. |
| `reply_to` | Where its sender asks replies to go. |
| `bcc` | Only where the message keeps it: a draft, or some clients' sent copies. |
| `text` | Its plain-text body; empty when it has none. |
| `html_text` | Its HTML body as text, for a message with no plain text: what a reply quotes. Empty otherwise. |
| `html` | Its HTML body, sanitized; empty when it has none. |
| `held_images` | How many remote images are held back. Above 0 only in a Junk folder. |
| `parts` | Its other parts: attachments, and the images its HTML shows. |

A part:

```json
{"section": "2", "name": "invoice.pdf", "type": "application/pdf", "size": 48213, "listed": true}
```

`section` is where the server holds it, and what it is fetched by. `size` is in bytes, worked out
from the server's encoded size. `listed` is `false` for an image the HTML shows in place rather
than a file to list.

The `html` keeps formatting, tables, links and images, and has no scripts, event handlers, forms,
frames or style sheets. It is still a stranger's: show it only in a sandbox. Its images point at
this instance, by paths relative to it: an image the message carries at its part, and a remote one
at the [image proxy](#images). In a Junk folder a remote image is a blank `data:` image instead,
with its proxy address in `data-src`, so the sender learns nothing until somebody chooses to load
it.

```
GET /api/email-configs/{id}/mailboxes/{mailbox}/messages/{message}/parts/{section}
```

One part's bytes, decoded, fetched alone. `section` is a part's `section`, like `2` or `1.3`. A
PNG, JPEG, GIF, WebP or AVIF image comes with its own `Content-Type` and
`Content-Disposition: inline`; anything else as `application/octet-stream` and `attachment`.
Either way the filename is in `Content-Disposition`. A part never changes, so it is served
`immutable`. A `section` that is not one answers `400 invalid`, one the message does not have
`404 not_found`, and a part that cannot be decoded `422 unreadable`.

```sh
curl -b jar -c jar -o invoice.pdf \
  "https://emguio.example.com/api/email-configs/$EC/mailboxes/$MB/messages/$MSG/parts/2"
```

## Search

A search is `q` on a folder's list of messages. The mail server's `SEARCH` runs it in that one
folder, INBOX included, and it is paged like any list. A server with an index of its own answers
fast; one without reads every message and takes as long.

`q` is words apart by spaces, and a message has to match every one.

| term | matches |
| --- | --- |
| `invoice` | the word anywhere in its headers or its text |
| `"next week"` | the words together |
| `from:bob` | `bob` in From |
| `to:ann` | `ann` in To or Cc |
| `subject:lunch` | `lunch` in Subject |
| `is:unread`, `is:read`, `is:starred` | its flags |

Quotes keep words together after an operator too: `from:"Bob Jones"`. Operators are not case
sensitive. Any other word with a colon in it, a link say, is a plain word. How a word matches —
case, part of a word — is the server's. A `q` of nothing but spaces is no search.

```sh
curl -b jar -c jar -G --data-urlencode 'q=from:bob is:unread invoice' \
  "https://emguio.example.com/api/email-configs/$EC/mailboxes/$MB/messages"
```

## Conversations

A conversation is the mail server's `THREAD`: by References and In-Reply-To, then by subject for
what has neither. Dovecot, Cyrus and most servers of their kind have it; Gmail does not, and there
every message stands alone. Lists stay a message a row, and these two calls add the grouping.

```
GET /api/email-configs/{id}/mailboxes/{mailbox}/threads
```

The query parameter `messages` is up to 200 of the folder's message ids, apart by commas.

```json
{"counts": {"1700000000-4521": 3}}
```

For each message asked about that is in a conversation with others in this folder, how many
messages that conversation holds here. A message on its own is left out, as is an id from another
UIDVALIDITY. `{}` on a server without THREAD.

```
GET /api/email-configs/{id}/mailboxes/{mailbox}/messages/{message}/conversation
```

The conversation the message is in, oldest first, by the date each message gives itself or else
by when it arrived:

```json
{"messages": [{"id": "1700000000-4519", "mailbox": "mb_…", "subject": "Lunch", …}], "earlier": 0}
```

Each is a list's message object with its folder's `mailbox`. They are the messages of this folder
threaded with it, the message itself included, and the messages of Sent that answer one of them
or that one of them answers; for a message in Sent, the other side comes from INBOX. A message
filed in both shows once. At most the newest 100 of this folder's are given, and `earlier` is how
many older ones were left out. Empty, with `earlier` 0, on a server without THREAD.

## Actions (jobs)

Everything done to a message — read or unread, starred or not, moved, deleted — is a job. A job is
answered as soon as it is queued, and emguio does it on the mail server in the background: whether
or not whoever asked is still connected, and across a restart.

```
POST /api/jobs
```

The body is `{"jobs": [ … ]}`, from 1 to 500 jobs. All are queued or none: one that cannot be
refuses the request, with `400` or `404`, and nothing is queued.

```json
{"jobs": [
  {"email_config": "ec_…", "mailbox": "mb_…", "message": "1700000000-4521", "kind": "seen", "value": true},
  {"email_config": "ec_…", "mailbox": "mb_…", "message": "1700000000-4520", "kind": "move", "target": "mb_…"}
]}
```

| field | |
| --- | --- |
| `email_config` | The message's email config. One request may hold jobs for several. |
| `mailbox` | The message's folder. |
| `message` | The message's id in that folder. |
| `kind` | `seen`, `flagged`, `move` or `delete`. |
| `value` | For `seen`, `true` marks it read and `false` unread. For `flagged`, `true` stars it and `false` takes the star off. Not used otherwise; leave it `false`. |
| `target` | For `move`: the folder it goes to, of the same email config, selectable, and not the one it is in. Not used otherwise. |
| `seen` | Whether the message was read when asked, as the caller last saw it. It is kept with the job and given back, for whoever draws the folders' counts while the job waits; the job itself goes by what the server says. Send the message's `seen`, or leave it out. |

Archiving, Trash and spam are moves: to the folder whose `special_use` is `archive` — `all` on
Gmail, where that is what archiving is — `trash`, or `junk`. `delete` deletes for good; it does
not go through Trash.

```sh
curl -b jar -c jar -H 'Content-Type: application/json' --data @- \
  https://emguio.example.com/api/jobs <<EOF
{"jobs": [{"email_config": "$EC", "mailbox": "$MB", "message": "$MSG", "kind": "seen", "value": true}]}
EOF
```

The answer is `202` with the jobs queued, in order:

```json
{"jobs": [{"id": "j_01k6r2s4t6v8w0x2y4z6a8b0c2", "email_config": "ec_…", "mailbox": "mb_…",
  "message": "1700000000-4521", "kind": "seen", "value": true, "target": "", "seen": false,
  "error": "", "created_at": 1790000000}]}
```

How they are done:

- Each email config's jobs are done one at a time, in the order asked. Jobs in a row that do the
  same to messages of one folder are done together, up to 500, in one command to the server.
- A `seen` or `flagged` job replaces one of the same kind still waiting on the same message: only
  the last counts. A move or a delete is never replaced.
- A try that could not reach the server is tried again after 5 seconds, then 30, 2 minutes and
  10 minutes. A server that refuses, or the last try failing, fails the job.
- A job on a message no longer there fails, except a `delete`, which is then done.
- Moving needs MOVE or UIDPLUS on the server, and deleting needs UIDPLUS. Without them those jobs
  fail, saying so.
- A job that is done leaves the list. One that failed stays, with its `error`, until it is
  dismissed, or for a day.

```
GET /api/jobs
```

`{"jobs": [ … ]}`: the user's jobs still waiting, with `error` empty, and those that failed, with
`error` the sentence saying why — oldest first, across every email config. A job is done once it
is no longer listed; a program that needs to know polls this.

```
DELETE /api/jobs/{job}
```

Dismisses a failed job. `204`. A job still waiting cannot be taken back, since it may already be
on the server: it answers `404 not_found`, as does one that is not there.

## Sending

```
POST /api/email-configs/{id}/send
```

Sends a message from the email config through its outgoing server, and answers once that server
has taken it. Nothing is queued. A server that refuses, or cannot be reached, answers
`502 unreachable` with its reason, and nothing has gone. A `200` means it has, and cannot be
undone:

```json
{"message_id": "<5f2b8c0e9a1d4e7f8b3c6a2d1e0f9b8c@example.com>"}
```

`message_id` is the `Message-ID` header it went with. Sending runs to the end even when the caller
leaves first, for at most two minutes.

| field | |
| --- | --- |
| `to`, `cc`, `bcc` | As typed: addresses apart by commas, each with a name or without, like `Bob Jones <bob@example.org>, ann@example.com`. At least one address across the three. |
| `subject` | Runs of spaces and line breaks become one space. |
| `text` | The body, as plain text. |
| `attachments` | New files: `[{"name": "a.pdf", "type": "application/pdf", "data": "…"}]`, with `data` in standard base64. An empty `name` becomes `attachment`, and a `type` that is not a media type `application/octet-stream`. |
| `reply` | `{"mailbox": "mb_…", "message": "…"}`: the message this answers. It goes with `In-Reply-To` and `References` from that message, which is then marked answered. One gone from the server since is answered unthreaded. |
| `carry` | `{"mailbox": "mb_…", "message": "…", "parts": ["2", "3"]}`: a message on the server whose parts, by `section`, go with this one as attachments — the one it forwards, or the draft it was opened from. |
| `draft` | `{"mailbox": "mb_…", "message": "…"}`: a draft on the server this was opened from. Once this has gone it is deleted, and without `reply` this is threaded as it was. |
| `draft_id` | The draft kept in emguio that this is; see [Drafts](#drafts). Once this has gone the draft is removed, and its copy in Drafts deleted. |
| `parts` | The ids of the attachments that draft holds that go with this one, in order. |
| `close` | Not used here. |

Mail goes from the email config's `email`, with its `sender_name`. emguio adds no `Re:`, quote or
forwarded header: what `subject` and `text` say is what is sent. Attachments go in this order: the
draft's, then those carried, then new ones. Together they may come to 25 MB; more answers
`413 too_large`. The body may be up to 36 MB, base64 being a third larger than the files.

Once it has gone, in the background, a copy is filed in the email config's Sent folder — unless,
three seconds on, the server has filed one itself with that Message-ID — the message replied to
is marked answered, and the draft it was written in is deleted from Drafts.

An email config with no outgoing server answers `409 conflict`. A field holding something that is
not an address answers `400 invalid`, naming it, as do no address at all and a `parts` id the
draft does not hold. A `draft_id` no longer kept, or another email config's, answers
`404 not_found`, and one that is not a draft id `400 invalid`. A carried message no
longer on the server answers `404 gone`, and a carried part that cannot be decoded
`422 unreadable`.

```sh
curl -b jar -c jar -H 'Content-Type: application/json' \
  -d '{"to":"bob@example.org","subject":"Lunch","text":"Thursday at one?"}' \
  "https://emguio.example.com/api/email-configs/$EC/send"
```

## Drafts

A draft is kept in emguio while it is written, and written to the email config's Drafts folder on
the mail server every so often. It needs a Drafts folder — a mailbox whose `special_use` is
`drafts` — and a server with UIDPLUS.

```
POST /api/email-configs/{id}/drafts
PUT /api/email-configs/{id}/drafts/{draft}
```

Both take the body `send` takes, and in a draft the address fields need not be addresses yet, nor
name anybody.

`POST` makes a draft. It reads what the draft names on the mail server now, once: the message
`reply` answers, to thread it, and the parts `carry` names, which the draft holds from then on.
`draft` is the copy on the server this replaces. `draft_id` is not used, and `parts` must be empty.

`PUT` saves the draft `{draft}`: `to`, `cc`, `bcc`, `subject` and `text` as given, then the
attachments it holds that `parts` names, in that order — those not named are dropped — then the
new `attachments`. `reply`, `carry`, `draft` and `draft_id` are not used.

```json
{"id": "d_01k6r2t5v7w9x1y3z5a7b9c1d3",
 "parts": [{"id": "dp_01k6r2t5v7w9x1y3z5a7b9c1d4", "name": "a.pdf", "type": "application/pdf", "size": 48213}],
 "problem": ""}
```

The answer, `200`, holds the draft's id, every attachment it holds now, in order, and `problem`:
why it could not be written to the mail server the last time it was tried, empty when it could.
Name the attachments to keep in `parts` on the next save rather than sending them again.

A save is answered at once. emguio writes the draft to Drafts thirty seconds after a save, and no
more often than that while saves keep coming: a new copy, marked read and `\Draft`, and then the
copy before it deleted. A write the server would not take is tried again after 30 seconds, 2
minutes, 10 minutes and then every hour. A draft nobody has saved for a week is removed from
emguio.

With `"close": true`, the draft is written to Drafts now, and emguio is done with it. The answer
is then one of:

- `200 {"closed": true}`: it is in Drafts.
- `202 {"closed": false, "problem": "…"}`: the server could not be reached. The draft stays in
  emguio and is written once it can be.
- `409 conflict`: there is nowhere to write it — no Drafts folder, or no UIDPLUS. A draft
  saved before stays in emguio, to be discarded; one made by this `POST`, whose id was never
  given, is removed.

To send a draft, post its fields to `send` with its `draft_id` and its attachments in `parts`.

A first save for an email config with no Drafts folder answers `409 conflict`. Attachments over
25 MB answer `413 too_large`, a `parts` id the draft does not hold `400 invalid`, as does a
`{draft}` that is not a draft id, and a draft no longer kept `404 not_found`.

```
DELETE /api/email-configs/{id}/drafts/{draft}
```

Discards the draft from emguio. Its copy in Drafts on the mail server is left, and the answer says
where it is:

```json
{"kept": {"mailbox": "mb_…", "message": "1700000000-77"}}
```

`kept` is `null` for a draft never written there. To delete that copy too, post a `delete` job
for it.

## Images

```
GET /api/proxy
```

Fetches a remote image a message's HTML shows, so its sender learns that the message was opened
but not where from or with what. Its query is `u`, the image's address in base64url, and `s`,
emguio's signature for it. Only addresses emguio signed are fetched, and those come only in the
`src` and `data-src` of a message's `html`, as `/api/proxy?s=…&u=…` with the query filled in (in
the HTML, `&` is written `&amp;`). Any other address answers `404 not_found`. The session cookie
is needed, as everywhere.

The image is fetched from public addresses only, within fifteen seconds and following at most two
redirects. It is served when its bytes — whatever its server said — are a PNG, JPEG, GIF, WebP,
AVIF, BMP or icon, and at most 10 MB; otherwise `415 unsupported_media_type` or `413 too_large`,
and `502 unreachable` for one that could not be fetched. Each user may fetch a burst of 200, then
ten a second; past that, `429 rate_limited`. An image is served `immutable`.

## Events

```
GET /api/events
```

A stream of Server-Sent Events saying that something the user can see has changed: mail emguio
keeps, a folder's counts, a run of jobs done, a draft written. Every event is the same:

```
event: changed
data: 1
```

It carries nothing else: whoever listens reads again what it shows. Events are at least a second
apart, and changes closer together are one event. A comment line, `: ping`, comes every 25
seconds so nothing between closes the stream. A program usually has no need for it: it can ask
for what it needs when it needs it.

```sh
curl -N -b jar -c jar https://emguio.example.com/api/events
```

## Session and invitations

```
GET /api/auth/me
```

The user signed in: `{"id": "u_…", "username": "ann", "created_at": 1790000000}`.

```
POST /api/auth/logout
```

Ends this session and clears the cookie. `204`. The user's other sessions stay signed in.

A user joins by invitation. Whoever runs the instance prints a link with `emguio invite`; it ends
in `/invite/{token}`, makes one user, and is good for seven days. Neither call below needs a
session.

```
GET /api/auth/invites/{token}
```

Whether the link is still good: `200 {"expires_at": 1790600000}`, or `404 not_found` for one
used, expired or never given out, alike.

```
POST /api/auth/invites/{token}/accept
```

Makes the user and signs them in. The body is `{"username": "…", "password": "…"}`, and the
answer `204` with the session cookie set. The username, trimmed, is 1 to 64 bytes with no control
characters; one taken already, without regard to case, answers `409 conflict` and leaves the link
unspent. The password is 8 to 72 bytes.

## Errors

Every refusal has the same shape, with the status that suits it:

```json
{"ok": false, "code": "invalid", "message": "limit is a number from 1 to 200."}
```

`code` is the contract: a program decides what to do by it, and by nothing else. `message` is a
sentence for a person — what went wrong, naming the limit or the value — and may change; show it,
do not match on it. Two codes can share a status: `gone` and `not_found` are both `404`.

| code | status | when |
| --- | --- | --- |
| `invalid` | 400 | A body that is not the JSON expected, or has a field this page does not list; a malformed id; a parameter out of range; a field that fails validation. |
| `unauthenticated` | 401 | No live session. At sign-in, a username and password that do not match. |
| `not_found` | 404 | No such thing, or not this user's: an email config, folder, draft, failed job, invitation, message part, image address or endpoint. |
| `conflict` | 409 | A username taken; a mail account with no outgoing server to send through, or no Drafts folder to keep a draft in; a draft closed with nowhere to write it. |
| `forbidden` | 403 | A request that changes something, sent with `Sec-Fetch-Site: cross-site`. |
| `unsupported_media_type` | 415 | A request body that is not `application/json`; a proxied image whose bytes are not an image. |
| `body_too_large` | 413 | A request body over 1 MB, or over 36 MB for sending and drafts. |
| `too_large` | 413 | Attachments over 25 MB; a proxied image over 10 MB. |
| `gone` | 404 | A message, folder or list cursor the mail server no longer has as it was named: expunged, moved, or in a folder whose UIDVALIDITY has changed. List the folder again. |
| `unreachable` | 502 | The mail server could not be reached, or refused, with its reason in `message`; an image could not be fetched. `503` when the instance is running without its mail reader. |
| `unreadable` | 422 | A message part, or a part carried into a message, that cannot be decoded. |
| `rate_limited` | 429 | Too many sign-in attempts, connection tests or images. Wait, then try again. |
| `busy` | 503 | A request that changes something, stopped after two minutes of waiting. Try it again. |
| `internal` | 500 | A fault in emguio. The message does not say what. |
