# Reading mail

emguio keeps a copy of each email config's mailboxes and message headers in its own database,
and the interface reads that copy. The mirror keeps it in step with the server.

## Read state is the server's

Whether a message is read lives on the server, and emguio follows it: reading a message here
marks it read there, as any mail client does, and marking it unread clears the flag there.

Everything else only reads. A sync opens mailboxes with `EXAMINE` and fetches headers and
bodies with `BODY.PEEK`, so a sync never changes a flag. The one write is `\Seen`, set or
cleared with `STORE` on the reading session, through `PATCH …/messages/{id}` with
`{"seen": true|false}`. The server is told first, and the store only once the server has taken
it.

A request of its own rather than something reading does on the side: a GET that changes things
is one a prefetch, or a link from anywhere, can make on somebody's behalf.

The reading pane sends it once per opening, when an unread message has loaded. A message marked
unread again stays unread while it is open, however often it is fetched; the next opening marks
it read.

## One worker per email config, holding one connection

The mirror starts a worker for every email config and keeps it signed in. A connection that
drops is opened again after a pause that grows with each failure in a row — thirty seconds up to
fifteen minutes — and a pass that gets through resets it. A config that is edited gets a new
worker, so it never goes on signing in with what was saved before.

The latest failure is the config's `sync_error`, in a sentence, shown above the list and cleared
by the next pass that works.

## INBOX every minute, everything every five

A quick pass looks at INBOX; a full pass lists the mailboxes and looks at all of them. Fetch new
mail, and saving a config, ask for a full pass now.

Each mailbox starts with `STATUS`: when the message count, the next UID, the unseen count and
UIDVALIDITY all match the last look, nothing that changes them has happened, and the mailbox is
not opened at all. That makes a pass over fifty quiet folders fifty short commands.

A flag changed elsewhere without changing the unseen count — a star — changes no number `STATUS`
reports. A full pass looks at INBOX's flags whatever `STATUS` says, so a star in INBOX arrives
within five minutes. In other mailboxes it arrives with the next change that does move a number.
`CONDSTORE`, which would say exactly which flags moved, is not used yet.

## Expunges are found by asking which UIDs are there

When a mailbox has changed, its UIDs are listed (`UID SEARCH ALL`, or `ESEARCH` where the server
has it) and compared with the stored ones: a stored UID the server no longer lists was expunged
and is dropped, and one the server lists that is not stored is new.

New messages are fetched newest first, five hundred at a time, and each batch is stored and
announced as it lands, so a large mailbox fills from the top while the rest arrives.

## UIDVALIDITY changing drops the mailbox's copy

A new UIDVALIDITY is the server saying every UID in the mailbox now names something else. No
stored row can be matched to anything, so all of them go and the mailbox is copied again.

## Pointing a config at another account drops its copy

Another incoming host or another username is another mailbox store, and what was copied from the
old one is not this config's mail any more. A new port or security setting is the same server,
and the copy stays.

## A message is listed by when it arrived

The list is ordered by the server's internal date — when the message arrived — and shows it. The
`Date` header is the sender's claim, which spam and misconfigured clients get wrong; it is kept
beside it, as `sent`.

A page is a position rather than an offset: mail arriving between two pages does not shift the
second one by however many came in.

## What a special mailbox is for comes from the server, then from its name

`SPECIAL-USE` flags name Sent, Drafts, Trash, Junk and Archive where the server has them. Where
it does not, a mailbox called Sent, Sent Items, Отправленные and the like is taken to be one —
once each, and never over a mailbox the server did flag.

## Headers are read in the charset they declare

koi8-r and windows-1251 are ordinary in the mail emguio is for, so encoded headers are decoded
with go-message's full set of charsets rather than the few the standard library knows. Mailbox
names arrive decoded from IMAP's modified UTF-7. Whatever is still not valid UTF-8 is replaced
before it is stored, because a single stray byte must not take a list down with it.

## The interface is told, not polled

The mirror tells the store when a user's mail moved, and the store tells that user's open tabs
over `/api/events`. The event carries nothing; the tab refetches what it is showing.

## A list's preview is read during the sync

With each batch of headers, the first 2 KB of every message's first text part — plain text if
there is one, else HTML — is fetched with `BODY.PEEK` and turned into a line: transfer encoding
and charset undone, wherever the fetch cut it off, tags dropped, quoted lines skipped. One fetch
per distinct part rather than per message, because in a batch most messages keep their text in
the same place. Reading the whole message later replaces the preview with one made from all of
it.

## A message is fetched whole when it is first opened

On a second session of its own, so opening a message never waits behind a sync copying thousands
of headers. That session is opened on demand and closed after five idle minutes. The message is
fetched with `BODY.PEEK[]` after checking that the mailbox's UIDVALIDITY is still the one its
UID belongs to. The fetch itself changes nothing; marking the message read is its own `PATCH`.
One the server no longer has is a `404 gone`, and asks the mirror for a look, so it leaves the
list too.

The raw message is kept, and every later opening and every attachment come from the copy.

## HTML from a message passes two walls

The first is the server: bluemonday keeps formatting, tables, the presentational attributes
newsletters lay themselves out with, links and images, and inline styles only for properties that
cannot name a URL. No script, no event handler, no form, no frame, no `<style>` sheet. Then every
image source is rewritten: a carried image (`cid:`) points at its part here, and a remote one is
held back.

The second is the browser: the HTML is shown only in an `<iframe sandbox>` without
`allow-scripts`, whose own CSP lets images come from this origin and nowhere else. Either wall
alone stops a script; both are kept because the first is code that can have a bug and the
second is a browser that can be old.

The frame never scrolls; the reading pane around it does. Its height is its content's, watched
as images arrive and as the pane changes width. Mail laid out wider than the frame — a 600px
table is the norm — is zoomed out until it fits, rather than cut off or scrolled sideways. It is
always on white: mail is written for a white page.

Without `<style>` sheets a newsletter that depends on them looks plainer than it should. Allowing
them safely means rewriting their `url()`s too, and that is not done yet.

## Remote images wait to be asked for

An image from elsewhere tells its sender that the message was opened, when and from where. They
are blocked by default — a blank image holds each one's place — and the reading pane says how
many. Showing them is a choice made per message, and they then load through the image proxy: the
sender learns that the message was opened, but not from where.

The proxy fetches only addresses it signed itself, with a key derived from the server key, so it
relays the images in messages and nothing else. It dials through the same screen as every mail
server, accepts only what is an image by its own bytes rather than by the header, and relays at
most 10 MB.

## A part is served so that it cannot act as a page here

An attachment is the sender's file. It is served with `nosniff` and a sandbox CSP, as a download
unless it is a plain image — PNG, JPEG, GIF, WebP or AVIF — so an HTML or SVG attachment can
never run as a page of this origin.

## How much is kept is not configurable yet

Every header in every mailbox is copied, and every message opened is kept whole, with no limit.
Retention is its own task.
