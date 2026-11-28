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

**Requests from other origins are refused.** A request with any method but `GET`, `HEAD` and
`OPTIONS` answers `403 forbidden` when it carries a `Sec-Fetch-Site` other than `same-origin` —
`same-site` too, since a page on a sibling subdomain is sent the cookie — and
`415 unsupported_media_type` when it has a body that is not `application/json`, but for a file
to import, sent as itself. A browser sets
`Sec-Fetch-Site` and a page cannot forge it; a program that sends no `Sec-Fetch-Site` at all is
fine. A request with no body, like a `DELETE`, needs no `Content-Type`. emguio sends no CORS
headers, so a page on another origin cannot read its answers, and `X-Frame-Options: DENY` on
every answer, so no page of it is shown in another's frame.

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
counts, and the headers of each one's newest messages, INBOX's 30 and every other's 10 — nothing
else. An older page of a folder, a search, a message, a part, a conversation and sending are each
asked of the mail server while the request waits. They take as long as the server does, and can answer
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
minutes. New mail is told to the user's [devices](#notifications) as the email config is set to.

An email config:

```json
{"id": "ec_01k6r2n8v4b7c9d3f5g1h6j8k2", "name": "Work", "email": "ann@example.com", "sender_name": "Ann Smith",
 "incoming": {"protocol": "imap", "host": "imap.example.com", "port": 993, "tls": "implicit", "username": "ann@example.com"},
 "outgoing": {"host": "smtp.example.com", "port": 465, "tls": "implicit", "username": ""},
 "created_at": 1790000000, "updated_at": 1790000000, "synced_at": 1790000300, "sync_error": "", "unseen": 3,
 "archive_mailbox": null, "notify": "now", "digest_every": 60}
```

| field | |
| --- | --- |
| `name` | A label for it. May be empty, and the header then shows the address. The header puts it on two lines at its first `/`. |
| `email` | The address mail is sent from. |
| `sender_name` | The name sent beside the address. Empty sends the address alone. |
| `incoming` | The IMAP server. `protocol` is always `imap`. |
| `outgoing` | The SMTP server, or `null` for none, in which case it cannot send. An empty `username` signs in with the incoming server's username and password. |
| `tls` | `implicit`, TLS from the first byte (usually ports 993 and 465), or `starttls` (usually 143 and 587). There is no unencrypted choice. |
| `synced_at` | When its mail was last brought up to date; `null` before the first time. |
| `sync_error` | Why the latest try did not, in a sentence; empty when it did. |
| `unseen` | How many messages are unread in the folders it notifies from — INBOX and the user's own unless chosen otherwise, see [Notifications](#notifications). Its badge in the interface. |
| `archive_mailbox` | The folder chosen to archive to; `null` for the one the server names. See below. |
| `notify` | How its new mail is told to the user's devices: `now`, each message as it arrives; `digest`, gathered; or `off`. `now` for a new one. See below. |
| `digest_every` | How many minutes a digest gathers: 30, 60, 120 or 240. 60 for a new one. |
| `off` | Whether it is turned off: kept, but neither read nor told about. `false` for a new one. See below. |
| `read_on_archive`, `read_on_trash`, `read_on_junk` | Whether its mail is marked read before it is archived, before it is deleted, and before it is marked as spam; `null`, as for a new one, follows the user's reading settings. See below. |

No answer ever carries a password.

```
GET /api/email-configs
```

`{"email_configs": [ … ]}`, in the user's order: those put in order first, in it, then the rest
oldest first, so a new one is last.

```
PUT /api/email-configs/order
```

Puts the user's email configs in an order: `{"ids": ["ec_…", "ec_…"]}`. They go first, in that
order, and the others follow as they were. Every list of them follows it — Settings, the account
picker. The answer is `200` with every one, as listed above. One twice or none at all answers
`400 invalid`; one that is not the user's, `404 not_found`.

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
PUT /api/email-configs/{id}/off
```

Turns the email config off, `{"off": true}`, or on again, `{"off": false}`. Off, emguio stops
reading it, tells nothing of its mail, and keeps everything else of it: its passwords, folders,
choices and place in the order. Every request that would reach its mail server — a folder's
messages, a message, a search, sending, drafts, jobs, making or emptying a folder, a fetch, and a
transfer to it — answers `409 conflict`, saying it is turned off. Its settings can still be
changed. On again, it is read from the start. Answers `200` with the email config.

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
PUT /api/email-configs/{id}/reading
```

Chooses whether the email config's mail is marked read before it is archived, before it is
deleted, and before it is marked as spam: `{"read_on_archive": true, "read_on_trash": null,
"read_on_junk": false}`, each `true`, `false`, or `null` to follow the user's, below. All are set
at once, and one left out is `null`. Answers `200` with the email config.

```
GET /api/reading
PUT /api/reading
```

The user's reading settings, for every email config that does not choose for itself:
`{"read_on_archive": false, "read_on_trash": false, "read_on_junk": false}`, all `false` for a new
user. `PUT` takes the same and answers with it. emguio's own pages apply them: an unread message
archived, deleted or marked as spam is asked read and then moved, in one request of jobs. A program moving mail asks for what it wants
itself.

```
PUT /api/email-configs/{id}/notifications
```

Chooses how new mail in the email config is told to the user's devices, and in which folders:

```json
{"notify": "digest", "digest_every": 120, "mailboxes": {"mb_01k6r2p3q5s7t9v1w3x5y7z9a1": false}}
```

`notify` is `now`, `digest` or `off`. `digest_every` is 30, 60, 120 or 240 minutes; `0`, or leaving
it out, keeps the one set. `mailboxes` turns folders' notifications on or off by id; the folders it
does not name stay as they are, and each folder's `notify` says where it stands. Answers `200` with
the email config. A value that is not one of these answers `400 invalid`, a folder that is not the
config's `404 not_found`.

With `now`, each new message is told as emguio sees it arrive, and more than three at once as one
notification counting them. With `digest`, nothing is told until the first new message has waited
`digest_every` minutes; then everything that arrived since is told as one. What was read, moved or
deleted meanwhile is left out of it. `off` tells nothing, and what was waiting is dropped.

New mail is a message that came into a folder since emguio last looked at it, unread, and dated
by the server since then: mail filed in from another folder keeps its first date, and is not new.
One message in two folders at once, as Gmail files one under a label and in INBOX, is told once.

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
finds shows in the folders, INBOX's list, `synced_at` and `sync_error`, and on the event stream,
which says the look is done even when it found nothing.

## Folders and messages

Reading INBOX is: list the folders, take the one whose `special_use` is `inbox`, list its
messages, and read one by its id.

```
GET /api/email-configs/{id}/mailboxes
```

`{"mailboxes": [ … ]}`, read from what emguio keeps, so it answers at once. Empty until the first
look after the email config is saved. In the order a sidebar shows them: as a tree, each folder
followed by the ones inside it. Side by side, the server's own come first — INBOX, Drafts, Sent,
Archive, All Mail, Flagged, Junk, Trash, then any holding one, like Gmail's `[Gmail]` — then the
user's in the order they were put, then the rest by name.

```json
{"id": "mb_01k6r2p3q5s7t9v1w3x5y7z9a1", "name": "INBOX", "path": ["INBOX"], "special_use": "inbox",
 "selectable": true, "messages": 1204, "unseen": 3, "uid_next": 4522, "notify": true}
```

| field | |
| --- | --- |
| `name` | The server's name for it. |
| `path` | `name` split at the server's delimiter: where it sits in the tree. |
| `special_use` | What it is for: `inbox`, `drafts`, `sent`, `scheduled`, `archive`, `all`, `flagged`, `junk`, `trash`, or empty. From the server's SPECIAL-USE flags, or else from a usual name like "Sent Items". `scheduled` is mail the server holds to send later (RFC 9979), and has no usual name. `other` is one the server keeps for itself for a use emguio has none for, like Yandex's Outbox: flagged with a flag emguio does not know, or called Outbox or Templates. It is not renamed, moved or deleted. |
| `selectable` | `false` for one that only holds other folders and has no messages of its own. |
| `messages`, `unseen` | How many messages it holds and how many are unread, at the last look. |
| `uid_next` | Grows with every message put in it, so a change shows even when the counts end up the same. |
| `notify` | Whether its new mail is told to the user's devices. Until chosen with `PUT …/notifications`, `true` for INBOX and the user's own folders, `false` for the server's others. |

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
PUT /api/email-configs/{id}/mailboxes/{mailbox}
```

Renames a folder, or moves it — inside another, or to the top — or both, with the body that makes
one: `{"name": "…", "parent": "mb_…" or ""}`. What is inside it goes with it, and every folder
keeps its id, so links and waiting jobs go on naming them. The answer is `200` with the folder.
The same name rules apply, and a folder cannot go inside itself or a folder inside it
(`400 invalid`). INBOX and the server's own folders — any with a `special_use` — keep their names
and places: `409 conflict`.

```
DELETE /api/email-configs/{id}/mailboxes/{mailbox}
```

Deletes a folder from the mail server. One that holds messages or other folders answers
`409 not_empty`, its message saying what — `Reading list holds 1 folder and 12 messages.` —
unless `?with=mail` asks for them to go too: the mail of each folder to Trash, as deleting does,
marked read first with `&read=1`, or for good from Trash or where there is none; then the folders,
the deepest first. One of the server's own, or one with one of them inside, answers
`409 conflict`. `204`.

```
POST /api/email-configs/{id}/mailboxes/{mailbox}/empty
```

Deletes for good every message in Trash or Junk, as the folder holds them when it is opened:
mail that arrives meanwhile stays. Done on the mail server before it answers `204`, not as a job.
Any other folder answers `400 invalid`.

```
POST /api/email-configs/{id}/mailboxes/{mailbox}/read
```

Marks read every message in the folder, as it holds them when it is opened: mail that arrives
meanwhile stays unread. Done on the mail server before it answers `204`, not as a job. A folder
that only holds other folders answers `400 invalid`.

```
POST /api/email-configs/{id}/mailboxes/{mailbox}/delete-old
```

Deletes what arrived in the folder more than some days ago, by the date the server holds it
from: `{"days": 30, "read": true}`, days from 1 to 3650. It goes to Trash, as deleting a message
does, marked read first when `read` is set; from Trash, or on a server with none, it is deleted
for good. Done on the mail server before it answers `200 {"messages": 12, "trash": true}`: how
many went, and whether to Trash. A folder that only holds other folders, or days out of range,
answers `400 invalid`.

```
PUT /api/email-configs/{id}/mailboxes/order
```

Puts the user's folders side by side in an order: `{"ids": ["mb_…", "mb_…"]}`, all with the same
parent. They go first among the user's, in that order, and the others beside them follow as they
were. The order is kept in emguio, not on the mail server. The answer is `200` with every folder,
as listed above. Ids inside different folders, or one twice, answer `400 invalid`; one of the
server's own, which keep their places, `409 conflict`.

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
{"messages": [ … ], "next_cursor": "1700000000-4471-1789990000"}
```

`next_cursor` is there only while older messages remain; pass it, with the same `q`, for the next
page. It names the last message of the page and when it arrived, and the next page goes on after
it, so mail arriving or leaving in between does not shift it. A cursor from before the folder's UIDVALIDITY changed answers `404 gone`: start the list
again without one.

A folder's first page, without `q`, is the newest messages emguio keeps of it — INBOX's 30, any
other's 10: it answers at once, whatever `limit` says. Every later page, a folder not looked at
yet, and a search are read from the mail server.

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
| `newsletter` | Whether it names a web address to unsubscribe at, in `List-Unsubscribe`. |

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
| `unsubscribe` | How its sender says to stop their mail, `null` when it says nothing: `{"link": "https://…", "one_click": true}`. `link` is their page; `one_click` is whether `POST …/unsubscribe` does it here instead. |

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
POST /api/email-configs/{id}/mailboxes/{mailbox}/messages/{message}/unsubscribe
```

Unsubscribes from a newsletter that takes one click: emguio sends its sender's server the POST RFC
8058 describes, to the address the message names on the mail server, and answers `204` once the
sender has answered `2xx`. No body. A message without one click answers `409 conflict`: open its
`link` instead. A sender that cannot be reached, or answers otherwise, is `502 unreachable`,
saying what it answered. At most ten a minute; past that, `429 rate_limited`.

```
GET /api/email-configs/{id}/mailboxes/{mailbox}/messages/{message}/parts/{section}
```

One part's bytes, decoded, fetched alone. `section` is a part's `section`, like `2` or `1.3`. A
PNG, JPEG, GIF, WebP or AVIF image, or a PDF, comes with its own `Content-Type` and
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
| `kind` | `seen`, `flagged`, `move`, `delete` or `transfer`. |
| `value` | For `seen`, `true` marks it read and `false` unread. For `flagged`, `true` stars it and `false` takes the star off. Not used otherwise; leave it `false`. |
| `target` | For `move`: the folder it goes to, of the same email config, selectable, and not the one it is in. For `transfer`: a folder of another of the user's email configs, selectable and not Scheduled. Not used otherwise. |
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

A `transfer` moves a message to another of the user's accounts: see
[Moving to another account](#moving-to-another-account).

The answer is `202` with the jobs queued, in order:

```json
{"jobs": [{"id": "j_01k6r2s4t6v8w0x2y4z6a8b0c2", "email_config": "ec_…", "mailbox": "mb_…",
  "message": "1700000000-4521", "kind": "seen", "value": true, "target": "", "transfer": "",
  "seen": false, "error": "", "created_at": 1790000000}]}
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

### Moving to another account

A `transfer` job moves its message to a folder of another of the user's email configs. Each
server holds only its own mail, so emguio carries it: it reads the whole message from the one,
appends it to the other with its flags and the date it arrived — unless a message there has its
`Message-ID` already — and then deletes it from the one for good. A stop between the two leaves
it in both, and the next try finds it there and only deletes it.

The `transfer` jobs of one request from one folder to one other are one transfer, named in each
job's `transfer`. Its messages go one at a time, each its own try of up to two minutes. A message
no longer there is passed over. A server that refuses — a day's uploads used up, a mailbox full,
no UIDPLUS to delete one message alone — stops the transfer: the messages still waiting stay
where they are, and their jobs end with the sentence as their `error`, as do a cancelled
transfer's with `Cancelled.`. A job that ends so is not failed for its own sake: a page shows the
transfer's outcome, not each job's.

```
GET /api/transfers
```

`{"transfers": [ … ]}`: the user's transfers, moving and finished, oldest first.

```json
{"transfers": [{"id": "tr_01k6r2s4t6v8w0x2y4z6a8b0c2", "email_config": "ec_…", "mailbox": "mb_…",
  "target_config": "ec_…", "target": "mb_…", "total": 1000, "moved": 340, "state": "moving",
  "error": "", "created_at": 1790000000}]}
```

`state` is `moving` while messages wait, then `done`, `cancelled`, or `stopped` with `error` the
server's sentence. `moved` counts those moved; one passed over is not counted. A finished transfer
stays listed until it is dismissed, or for a day.

```
POST /api/transfers/{transfer}/cancel
```

Leaves where they are the messages still waiting. What has moved stays moved, and the message
under way finishes. `200` with the transfer; one already finished is given back as it ended.

```
POST /api/transfers/{transfer}/retry
```

Sends again the messages a stopped or cancelled transfer left where they were, as if asked now:
after any job asked since. Its `error` is cleared and it is `moving` again, with what it moved
still counted. `200` with the transfer; one still moving is given back as it is. One that moved
everything answers `400 invalid`, and one whose account, or the account it goes to, is turned
off answers `409 conflict`.

```
DELETE /api/transfers/{transfer}
```

Dismisses a finished transfer, and the jobs it ended. `204`. One still moving answers
`400 invalid`: cancel it first.

## Exporting

```
POST /api/email-configs/{id}/mailboxes/{mailbox}/export
```

Asks for a folder's mail as a file. `{"format": "mbox", "messages": [], "subfolders": false}`:
`format` is `mbox` or `eml`; `messages` a selection of the folder's, or none for all of it, at
most 10000; `subfolders` the folders inside it too, not with a selection. `201` with the
export:

```json
{"id": "ex_01k6r2s4t6v8w0x2y4z6a8b0c2", "email_config": "ec_…", "mailbox": "mb_…",
  "state": "ready", "total": 1500, "exported": 0, "error": "", "file": "Archive.mbox",
  "url": "/api/exports/ex_01k6r2s4t6v8w0x2y4z6a8b0c2/file"}
```

`file` is always a `.zip`, of mbox files or of eml files; see [reading.md](reading.md) for what
is in it. `total` is how many there are as last counted.
An export is held for an hour, in memory: a restart forgets it.

```
GET /api/exports
```

`{"exports": [ … ]}`: the user's exports of the last hour, oldest first, each as below.

```
GET /api/exports/{export}
```

The export as above, `mailbox` the folder exported: `state` is `ready`, `exporting`, `done`, `failed` with `error` the
sentence, or `deleted`; `exported` counts the messages written so far.

```
GET /api/exports/{export}/file
```

The file, once, written as the messages are read: `Content-Disposition: attachment` and no
`Content-Length`. A second download answers `409 conflict`. A server that stops answering
partway cuts the download short rather than ending it as if whole.

```
POST /api/exports/{export}/delete
```

`{"read": false}` deletes what went into a `done` export, and nothing that arrived since: moved
to Trash, marked read first with `read`, or deleted for good from Trash or where there is none.
For a folder exported whole, the folders go too, deepest first, once empty; one that mail came
into since stays, with those it is in, and Inbox and the server's own folders stay.
`200 {"trash": 1500, "deleted": 0, "folders": 2, "kept": 0}`: how many messages went each way,
how many folders went, and how many stayed. Once: then the export is `deleted`.

```
DELETE /api/exports/{export}
```

Dismisses an export once it has been shown. `204`. One downloading goes on; what went into it can
no longer be deleted here.

## Importing

```
POST /api/email-configs/{id}/mailboxes/{mailbox}/import
```

The body is the file itself, not JSON, with its type: `application/mbox`, `message/rfc822` for
one `.eml`, or `application/zip` for a zip of them. `?name=Archive.zip` is the file's name, which
an eml's date is read from. Up to 10 GB, written to disk as it arrives; the request runs as long as the
upload does. `202` with the import, which then goes on in the background:

```json
{"id": "im_01k6r2s4t6v8w0x2y4z6a8b0c2", "email_config": "ec_…", "mailbox": "mb_…",
  "file": "Archive.zip", "state": "importing", "total": 1500, "imported": 340, "skipped": 2,
  "folders": 1, "error": "", "created_at": 1790000000}
```

An mbox or an eml goes into the folder; a zip is a tree inside it, folders made where they are
not there yet. See [reading.md](reading.md) for what is read from each. `state` is `importing`,
then `done`, `cancelled`, or `failed` with `error` the sentence. `skipped` counts messages the
folder had already, by `Message-ID`. A folder that holds only other folders answers `400 invalid`;
a file of another type `415 unsupported_media_type`.

```
GET /api/imports
```

`{"imports": [ … ]}`: the user's imports, running and finished, oldest first. A finished one is
listed until it is dismissed, or for a day; a restart forgets them all.

```
POST /api/imports/{import}/cancel
```

Stops an import: what went in stays. `200` with the import.

```
DELETE /api/imports/{import}
```

Dismisses a finished import. `204`. One still importing answers `400 invalid`.

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
| `send_at` | When the outgoing server is to send it, in Unix seconds; `0`, or leaving it out, is now. See below. |
| `cancels` | `{"mailbox": "mb_…", "message": "…"}`: a message in Scheduled this one goes in place of. See below. |

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

```
GET /api/email-configs/{id}/hold
```

Until when the email config's outgoing server will hold a message to send it later:
`{"latest": 1821000000}`, in Unix seconds, or `{"latest": null}` for a server that holds none, or
no outgoing server. It is the server's FUTURERELEASE (RFC 4865), the longest it holds and the
latest date it names, whichever is earlier. emguio signs in to ask, and keeps the answer ten
minutes. A server that cannot be reached answers `502 unreachable`.

With `send_at` on a send, the message is handed to the outgoing server at once, with
`HOLDUNTIL`, and the server sends it at that time, with no device on. It is dated `send_at`, and
its copy in Sent is filed now, dated `send_at` too. Nothing in emguio can call it back once it has
been handed over. A time less than a minute away answers `400 invalid`; a server that holds no
mail, or not that long, `502 unreachable` with its reason, and nothing has been handed over.

A server that shows the mail it holds in a `scheduled` folder, as mailio does, files it in Sent
itself when it goes, so emguio files no copy then. Deleting a message there cancels it, and
`cancels` on a send does that first: the message named is deleted from Scheduled, and only then
does this one go, so the two never both do. One no longer there — sent, or cancelled elsewhere —
answers `404 gone`, and nothing is sent. A message in any other folder answers `400 invalid`.

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

## Notifications

A device is a browser that agreed to show emguio's notifications. emguio hands each one, encrypted,
to the push service the browser named — Apple's, Google's, Mozilla's — which wakes the device.
The service sees how long a message is and when it was sent, never what it says. Every device of a
user is told about every email config's new mail, as each is set to.

```
GET /api/push/key
```

`{"key": "BHx…"}`: the instance's public key, base64url, which a browser subscribes with as its
`applicationServerKey`. It is the same for as long as the instance keeps its database.

```
POST /api/devices
```

Registers a browser's push subscription for the user signed in:

```json
{"endpoint": "https://fcm.googleapis.com/fcm/send/…", "p256dh": "BNc…", "auth": "tBH…",
 "label": "Chrome on Mac", "client_id": "5f1c…"}
```

`endpoint`, `p256dh` and `auth` are the subscription's, as `PushSubscription.toJSON()` gives them;
the endpoint is an https address. `label` names the device for a person, at most 100 characters.
`client_id` is the browser's own name for itself, at most 64 characters, kept by the page: a
browser renews its subscription on its own, and the one it had is replaced rather than kept beside
the new. The same endpoint registered again updates its row, and passes to whoever registered it
last. Answers `201` with the device. One that is not https, or without its keys, answers
`400 invalid`.

```
GET /api/devices
```

`{"devices": [ … ]}`, oldest first. A device:

```json
{"id": "dv_01k6r2t5v7x9z1b3d5f7h9k1m3", "client_id": "5f1c…", "label": "Chrome on Mac",
 "created_at": 1790000000, "last_ok_at": 1790000900, "failure_count": 0, "last_error": ""}
```

`last_ok_at` is when its push service last took a notification, `null` before the first.
`failure_count` is how many in a row it did not take since, and `last_error` what it said. A push
service saying the subscription is over deletes the device. No answer carries a device's endpoint
or keys: whoever holds them can show text on its screen.

```
DELETE /api/devices/{id}
```

Stops telling the device. `204`.

```
POST /api/devices/test
```

Sends a notification to every one of the user's devices: `200 {"delivered": 2}`, how many push
services took it. A burst of three, then one every twenty seconds; past that, `429 rate_limited`.

```
GET /api/push/settings
PUT /api/push/settings
```

How all of the user's notifications come, whichever email config and device they are for:

```json
{"silent": false, "quiet": {"from": "23:00", "until": "07:00", "zone": "Europe/Berlin"}}
```

`silent` asks each device to show them without a sound; Safari on iPhone and iPad does not take
it. `quiet` is the hours of every day nothing is sent, on a 24-hour clock in an IANA time zone,
past midnight when `from` is after `until`, and `null` for none. Mail that arrives in them is told
when they end, in one notification per email config, leaving out what was read meanwhile. A test
notification is sent all the same.

`PUT` takes the same and answers with what it kept. A time not written as `HH:MM`, the same time
twice, or a zone emguio does not know answers `400 invalid`.

## Events

```
GET /api/events
```

A stream of Server-Sent Events saying that something the user can see has changed, and which of
the user's email configs emguio is working on. Something changed — mail emguio keeps, a folder's
counts, a run of jobs done, a draft written — is always the same:

```
event: changed
data: 1
```

It carries nothing else: whoever listens reads again what it shows. These are at least a second
apart, and changes closer together are one event.

Which email configs emguio is at work on, on their mail servers — looking over their folders, or
doing a job or another change asked of it — comes at once on connecting, and again each time it
changes, at least a quarter of a second apart:

```
event: busy
data: ["ec_01k6r2t5v7x9z1b3d5f7h9k1m3"]
```

An empty list is none. A comment line, `: ping`, comes every 25 seconds so nothing between closes
the stream. A program usually has no need for it: it can ask for what it needs when it needs it.

```sh
curl -N -b jar -c jar https://emguio.example.com/api/events
```

## Session and invitations

```
GET /api/auth/me
```

The user signed in: `{"id": "u_…", "username": "ann", "created_at": 1790000000, "seal": "open"}`.
`seal` is `open` once they have unsealed since the instance started, `sealed` before, and `none`
for a user from before seal keys, who has yet to make one; see below.

```
PUT /api/auth/password
```

`{"current": "…", "password": "…"}` changes the user's password, given the current one: a
session is not knowing it. The new one is at least 8 characters. Every other session is signed
out and this one stays. `204`, or `400 invalid` for a current password that is not theirs. Tries
share sign-in's limit for the username; past it, `429 rate_limited`.

```
POST /api/auth/logout
```

Ends this session and clears the cookie. `204`. The user's other sessions stay signed in.

```
GET /api/sessions
```

The user's sessions, the most recently used first:

```json
{"sessions": [{"id": "s_3f9a1c0b7e2d", "current": true, "created_at": 1790000000,
  "last_seen_at": 1790003600, "expires_at": 1790608400, "device": "Firefox on a Mac",
  "user_agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Firefox/140.0",
  "ip": "203.0.113.7"}]}
```

`current` is the one this request came with. `device` is a guess at the browser and platform, and
`user_agent` what it was guessed from. `ip` is the address it was last used from, read through the
trusted proxies as [deploy.md](deploy.md) says, and noted as the session slides, at most hourly;
empty for a session from before it was kept. An `id` is derived from the session, never the cookie's
value. A session is good for seven days from when it was last used.

```
DELETE /api/sessions/{id}
```

Signs that session out. `204`; for the current one the cookie is cleared in the same answer.

```
DELETE /api/sessions
```

Signs out every session but the current one. `204`.

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

Makes the user, with their seal key, and signs them in. The body is
`{"username": "…", "password": "…", "seal_key": "…"}`, and the answer `204` with the session
cookie set. The username, trimmed, is 1 to 64 bytes with no control characters; one taken already,
without regard to case, answers `409 conflict` and leaves the link unspent. The password is 8 to
72 bytes. The seal key is at least 12 characters; a shorter one answers `400 invalid` and leaves
the link unspent.

## Seal key

Each user's mail passwords are sealed with a key of their own, opened by their seal key: a
passphrase apart from their password, which emguio never stores. After every start of the
instance a user is sealed until they unseal, and until then every call but the ones in this
section, signing out, the sessions, changing the password and `GET /api/auth/me` answers
`423 sealed`, and none of their mail is read. See [email-configs.md](email-configs.md).

```
POST /api/seal/unseal
```

`{"key": "…"}` opens the user's seal key until the instance stops, and their mail is read from
then on. `204`, or `400 invalid` for a key that is not theirs. Five tries, then one every twelve
seconds; past that, `429 rate_limited`.

```
PUT /api/seal
```

`{"current": "…", "key": "…"}` changes the seal key; the current one has to open it. The mail
passwords stay as they are. `204`, `400 invalid` for a wrong current key or a new one shorter than
12 characters, and limited as unsealing is.

```
POST /api/seal/reset
```

`{"key": "…"}` replaces a forgotten seal key with a new one, and opens it. Every mail password the
user saved was sealed with the old one, and each mail account asks for its passwords again; its
folders and settings are kept. `204`.

```
POST /api/seal
```

`{"key": "…"}` makes the first seal key of a user from before seal keys, and opens it: as `reset`,
their saved mail passwords are asked for again. `409 conflict` for one who has a key.

```
POST /api/seal/carry
```

`{"key": "…"}` is the other way for that user: the key the instance ran with before seal keys, as
`EMGUIO_SECRET_KEY` was, 32 bytes in base64. Every saved mail password it opens is sealed again
with a key of the user's own, and that old key is their seal key from then on, to be changed with
`PUT /api/seal`. `204`; `400 invalid` for a key that is not 32 bytes in base64, or that opens none
of the user's saved passwords; `409 conflict` for one who has a key. Limited as unsealing is.

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
| `not_found` | 404 | No such thing, or not this user's: an email config, folder, draft, failed job, device, invitation, message part, image address or endpoint. |
| `conflict` | 409 | A username taken; a mail account with no outgoing server to send through, or no Drafts folder to keep a draft in; a draft closed with nowhere to write it. |
| `not_empty` | 409 | A folder deleted that holds messages or other folders, without `?with=mail`. |
| `forbidden` | 403 | A request that changes something, sent from a page of another origin. |
| `unsupported_media_type` | 415 | A request body that is not `application/json`; a proxied image whose bytes are not an image. |
| `body_too_large` | 413 | A request body over 1 MB, or over 36 MB for sending and drafts. |
| `too_large` | 413 | Attachments over 25 MB; a proxied image over 10 MB. |
| `gone` | 404 | A message, folder or list cursor the mail server no longer has as it was named: expunged, moved, or in a folder whose UIDVALIDITY has changed. List the folder again. |
| `unreachable` | 502 | The mail server could not be reached, or refused, with its reason in `message`; an image could not be fetched. `503` when the instance is running without its mail reader, or without notifications. |
| `unreadable` | 422 | A message part, or a part carried into a message, that cannot be decoded. |
| `rate_limited` | 429 | Too many sign-in attempts, connection tests, images or test notifications. Wait, then try again. |
| `busy` | 503 | A request that changes something, stopped after two minutes of waiting. Try it again. |
| `sealed` | 423 | The user has not unsealed since the instance started; see [Seal key](#seal-key). |
| `internal` | 500 | A fault in emguio. The message does not say what. |
