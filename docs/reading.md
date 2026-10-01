# Reading mail

## Only INBOX's newest are kept, and everything else is read from the server

emguio keeps the least that lets a list open at once: each email config's mailboxes, with how
many messages each holds and how many are unread, and the headers of INBOX's newest 30 messages
— the window. No body, no attachment and no other mailbox's messages are kept.

Everything else is asked for when somebody asks: another folder, an older page of INBOX, a
message, one of its parts. It is slower than a copy, by a trip to the mail server, and there is
no copy to grow, to fall out of step or to leak.

The window is a constant, not a setting.

## Read state is the server's

Whether a message is read lives on the server, and emguio follows it: reading a message here
marks it read there, as any mail client does, and marking it unread clears the flag there.

A sync only reads. It opens mailboxes with `EXAMINE` and fetches headers and bodies with
`BODY.PEEK`, so a sync never changes a flag. What is written is what somebody asks for, on the
reading session: a flag through `PATCH …/messages/{id}`, a move, a delete. The server is told
first, and a kept row and the folders' counts only once the server has taken it.

Its own request rather than something reading does on the side: a GET that changes things is one
a prefetch, or a link from anywhere, can make on somebody's behalf.

The reading pane sends it once per opening, as soon as it knows the message is unread — from its
row in the list, before the rest has arrived. A message marked unread again stays unread while it
is open; the next opening marks it read.

The pane draws who, what and when from that row too while the message is fetched, and every
action works from then: only a message opened from a link, before its folder's list, waits for
the server.

## Archive, Trash and spam are moves to the server's own folders

Each is `POST …/messages/{id}/move` to the folder the server's `SPECIAL-USE` — or its name —
says is for it: Archive, or Gmail's All Mail where there is none, which on Gmail is what
archiving is; Trash; Junk, and from Junk back to INBOX. An action whose folder the server does
not have, or that the message is already in, is not offered. Any other folder is a move too.

Deleting moves to Trash. In Trash, or on a server with none, it is `DELETE …/messages/{id}`,
for good, and asked about first.

A move is `MOVE` where the server has it. Without it, it is `COPY`, `\Deleted` and an `EXPUNGE`
of that one UID, which needs `UIDPLUS`: a plain `EXPUNGE` would also remove whatever another
client had marked deleted, so on a server with neither, moving and deleting are refused. A star
is `\Flagged`, set and cleared like `\Seen`.

Gmail's keys work — `e` archive, `#` delete, `!` spam, `s` star, `u` read or unread — except
while a field has the keys.

## Every action is drawn before the server answers, over what the server said

Read, star, archive, delete, spam and move change the screen on the press — the button, the
list's row, the folders' counts — and the request goes after. What is drawn is two things: what
the server last said, kept as it said it, and the actions pressed and not yet answered, applied
over it as it is drawn. Nothing is written into the first until the server has confirmed it.

So a refusal has nothing to undo. The refused action stops being pending, and the screen is
what the server said, with every action pressed after it still drawn; a pile of actions with a
refusal at its end loses only that one. And a list read back while actions are queued — the
event stream asks for one after each — is drawn with them still applied, rather than showing
the messages they were pressed on as though nothing had been done.

The requests go one at a time per email config, in the order pressed, so a star and its undoing
reach the server as they were meant. A refused flag says why in the pane; a refused move, in a
notice, since the pane has gone on by then.

After a move or a delete the pane goes on to the message below it in the list, or else the one
above, or else back to the folder. The mirror is asked for a look: the window has a place to
fill, and the counts are the server's to confirm.

## A message is named by what the server calls it

Most messages have no row here to name them by, so a message's id is the server's own:
`{uidvalidity}-{uid}`, under its mailbox's `mb_` id — `/c/ec_…/mb_…/1700000000-4521`. IMAP
allows both only as positive 32-bit numbers, so the id is digits and a dash on every server. A
dash rather than a dot: a path ending in a dot and digits reads as a file to whatever serves it,
this server's own bundle included.

A server that renumbers a mailbox changes its UIDVALIDITY, and a link from before answers
`gone` rather than opening whatever has that UID now. A message moved to another folder has
another UID there, and its old link is gone too.

## One worker per email config, holding one connection

The mirror starts a worker for every email config and keeps it signed in. A connection that
drops is opened again after a pause that grows with each failure in a row — thirty seconds up to
fifteen minutes — and a pass that gets through resets it. A config that is edited gets a new
worker, so it never goes on signing in with what was saved before.

The latest failure is the config's `sync_error`, in a sentence, shown above the list and cleared
by the next pass that works.

## The window every minute, the counts every five

A quick pass brings the window up to date; a full pass also lists the mailboxes and asks each
for its counts with `STATUS`. Fetch new mail, and saving a config, ask for a full pass now.

The window is opened on every pass rather than only when `STATUS` has moved: a flag changed
elsewhere — a star — moves no number `STATUS` reports, and the newest 30 messages' flags cost
one short `FETCH`. A message that left the window, by being expunged or by newer mail pushing it
out, is dropped; one that entered it has its header fetched.

## UIDVALIDITY changing drops the window

A new UIDVALIDITY is the server saying every UID in the mailbox now names something else. No
kept row can be matched to anything, so all of them go and the window is filled again.

## Pointing a config at another account drops what is kept

Another incoming host or another username is another mailbox store, and what was kept from the
old one is not this config's mail any more. A new port or security setting is the same server,
and what is kept stays.

## A list is read a page at a time, by UID

INBOX opens on the window, from here. Every other mailbox, and INBOX past its window, is read
from the server, fifty at a time and newest to arrive first. The first page is the last messages
by sequence number, which needs only the count the mailbox opens with. A later page asks the
server which UIDs are below the last one shown, so mail arriving or leaving between two pages
does not shift the second.

A list shows the server's internal date — when the message arrived. The `Date` header is the
sender's claim, which spam and misconfigured clients get wrong; it is kept beside it, as `sent`.

## What a special mailbox is for comes from the server, then from its name

`SPECIAL-USE` flags name Sent, Drafts, Trash, Junk and Archive where the server has them. Where
it does not, a mailbox called Sent, Sent Items, Отправленные and the like is taken to be one —
once each, and never over a mailbox the server did flag.

## Text is read in the charset it declares

Nearly all mail is UTF-8. What declares another charset — GBK and ISO-2022-JP are still sent —
is read in it with go-message's decoders rather than the few the standard library knows,
headers and bodies alike, and nothing more is done for it. Mailbox names arrive decoded from IMAP's modified UTF-7.
Whatever is still not valid UTF-8 is replaced before it is stored, because a single stray byte
must not take a list down with it.

## The interface is told about what is kept, and asks for the rest

The mirror tells the store when a user's kept mail moved, and the store tells that user's open
tabs over `/api/events`. The event carries nothing; the tab refetches the mailboxes and INBOX's
list. A list read from the server, and the open message, are not refetched on an event: each
would be a trip to the mail server on every change. Fetch new mail asks for them again.

## A list's preview is read with its headers

With each run of headers, the first 2 KB of every message's first text part — plain text if
there is one, else HTML — is fetched with `BODY.PEEK` and turned into a line: transfer encoding
and charset undone, wherever the fetch cut it off, tags dropped, quoted lines skipped. One fetch
per distinct part rather than per message, because in a run most messages keep their text in
the same place.

A message that gives no line from that — an HTML part whose first kilobytes are its head and
styles, or a plain part that is empty beside an HTML one — is read once more, 32 KB of the same
part or the next one. Opening the message replaces a kept preview with one made from all of its
text.

## A message is opened without its attachments

On a second session of its own, so opening a message never waits behind a pass. That session is
opened on demand, closed after five idle minutes, and serves one request at a time.

Opening is two requests: the headers and `BODYSTRUCTURE`, the server's description of every part
— its type, name, size and section — and then the text alone, its plain and HTML parts by
section. The list of attachments comes from the description; an attachment's bytes come only
when somebody downloads it, so a message with a large one opens as fast as one without.

Both are fetched with `BODY.PEEK` after checking that the mailbox's UIDVALIDITY is still the one
the message's id names. Neither changes anything; marking the message read is its own `PATCH`.
One the server no longer has is a `404 gone`, and asks the mirror for a look, so it leaves the
list too.

A filename arrives as the server passes it on: decoded from encoded words by the client, and
from RFC 2231 — `filename*=utf-8'ru'%D0%9E…`, which Thunderbird writes any non-ASCII name in —
here.
An attachment's size in the list is the server's, in its transfer encoding, scaled back: about
three quarters for base64.

## A part is fetched alone, by its section

An attachment, or an image the message carries, is fetched by its IMAP section —
`BODY.PEEK[2.MIME]` and `BODY.PEEK[2]` — rather than with the rest of the message, so an image
costs its own bytes. A UID under one UIDVALIDITY names one message for good and a message never
changes, so a part is served `immutable` and the browser keeps it.

## HTML from a message passes two walls

The first is the server: bluemonday keeps formatting, tables, the presentational attributes
newsletters lay themselves out with, links and images, and inline styles only for properties that
cannot name a URL. No script, no event handler, no form, no frame, no `<style>` sheet. Then every
image source is rewritten: a carried image (`cid:`) points at its part here, and a remote one at
the image proxy.

The second is the browser: the HTML is shown only in an `<iframe sandbox>` without
`allow-scripts`, whose own CSP lets images come from this origin and nowhere else. Either wall
alone stops a script; both are kept because the first is code that can have a bug and the
second is a browser that can be old.

The frame fills the space under the headers, edge to edge with no margin of its own, and scrolls
itself while the headers stay above it: mail lays itself out to fill a page. Around HTML the pane
does not scroll, and the application is pinned to the window rather than as tall as it, so there
is one scroll on the screen; a plain message, which has no frame, scrolls the pane instead. A frame as tall as
its content, left to the pane to scroll, cannot be scrolled on a phone, because Mobile Safari does
not hand a touch on a frame to the pane around it.

The same browser widens a frame to what it holds, whatever width it is given. So the frame sits
in a box that clips it, and the pane never scrolls sideways; and the mail is fitted to the box's
width, measured outside the frame, rather than to the frame's own, which only says how wide the
mail already is.

Mail laid out wider than that — a 600px table is the norm — is zoomed out until it fits,
rather than cut off or scrolled sideways, and fitted again as images arrive and the pane changes
width. Whatever is still wider, for the moment before a fit, scrolls sideways inside the frame,
under headers that stay where they are. It is always on white: mail is written for a white page.

Without `<style>` sheets a newsletter that depends on them looks plainer than it should. Allowing
them safely means rewriting their `url()`s too, and that is not done.

## Remote images load through the proxy, except in Junk

An image from elsewhere tells its sender that the message was opened, and when: a tracking pixel
has an address of its own for each recipient. The proxy hides the rest — where the reader is,
what they read with — and only the opening is left. That is accepted, as Gmail accepts it, and
images load with the message.

Except in Junk, where loading an image also tells a spammer the address is read. There each is a
blank image until somebody asks — the reading pane says how many — with the proxy's address for
it in `data-src`, and asking swaps the addresses in where they stand, without fetching the
message again.

The proxy fetches only addresses it signed itself, with a key derived from the server key, so it
relays the images in messages and nothing else. The signature makes the address the same in
every message, with nothing stored, and it is served `immutable`: a logo in every newsletter is
fetched once. It dials through the same screen as every mail server, accepts only what is an
image by its own bytes rather than by the header, and relays at most 10 MB.

The frame's CSP names this origin as well as `'self'`: Firefox does not count a `srcdoc` frame's
inherited origin as `'self'`, and with that alone blocks every image in it.

## A part is served so that it cannot act as a page here

An attachment is the sender's file. It is served with `nosniff` and a sandbox CSP, as a download
unless it is a plain image — PNG, JPEG, GIF, WebP or AVIF — so an HTML or SVG attachment can
never run as a page of this origin.
