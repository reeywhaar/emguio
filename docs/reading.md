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
`BODY.PEEK`, so a sync never changes a flag. What is written is what somebody asks for, as a job:
a flag, a move, a delete. The mail server is told first, and a kept row and the folders' counts
only once it has taken it.

A job of its own rather than something reading does on the side: a GET that changes things is
one a prefetch, or a link from anywhere, can make on somebody's behalf.

The reading pane asks for it once per opening, as soon as it knows the message is unread — from its
row in the list, before the rest has arrived. A message marked unread again stays unread while it
is open; the next opening marks it read.

The pane draws who, what and when from that row too while the message is fetched, and every
action works from then: only a message opened from a link, before its folder's list, waits for
the server.

## Archive, Trash and spam are moves to the server's own folders

Each is a move job to the folder the server's `SPECIAL-USE` — or its name —
says is for it: Archive, or Gmail's All Mail where there is none, which on Gmail is what
archiving is; Trash; Junk, and from Junk back to INBOX. An action whose folder the server does
not have, or that the message is already in, is not offered. Any other folder is a move too.

Archive is the one a user may choose instead, on the mail account's card in Settings: for a
server that names none, or names one other than the one wanted. Saved with
`PUT /api/email-configs/{id}/archive` and nothing else about the account, so its sessions go on as
they are; a folder the server later drops goes back to the server's.

A folder is made from New, last in the folder list, which opens it once made, and from the folder
picker's New folder…, which then moves to it: a name, and where it goes — at the top of the user's
own folders, under the server's personal namespace (`INBOX.` on servers that keep everything under
INBOX), or inside another. `POST /api/email-configs/{id}/mailboxes` sends `CREATE` on the session
for changes, lists the folders again so the new one is kept like the rest, and answers with it;
the move then goes to it as to any other. A name holding the server's delimiter is refused rather
than read as nesting, which is what choosing where it goes is for, and so is one a sibling already
has.

A folder of the user's own — none of the server's, which keep their names and places — has a menu
beside its name above the list: rename or move, in the same dialog, and delete. Renaming is
`RENAME`, and so is moving, into another folder or to the top: what is inside goes with it. Here
the folder and those inside it are renamed in place before the folders are listed again, so each
keeps its id, and a link, a waiting job or the folder chosen to archive to goes on naming it.
Nothing goes inside itself. Deleting is `DELETE`, of an empty folder only: one holding mail, which
the server would delete with it, or other folders, is refused, with what to do first.

The server's own folders — those with a use, and those holding one, like Gmail's `[Gmail]` —
come first in a fixed order and stay there, with a wider gap after them. The user's follow, and
one is dragged up or down among the user's beside it, what is inside going along; a finger rests
on it first, since one that moves at once scrolls the list. Into another folder is a move, which
is the menu's. IMAP keeps no order, so the order is emguio's: a position per folder, set for all
of the user's beside it at once by `PUT /api/email-configs/{id}/mailboxes/order`. One never
placed comes after the placed ones, by name, and a folder moved elsewhere starts again among its
new neighbours. A rename on another client is a new folder here, with no place.

Deleting moves to Trash. In Trash, or on a server with none, it is a delete job, for good, and
asked about first.

A move is `MOVE` where the server has it. Without it, it is `COPY`, `\Deleted` and an `EXPUNGE`
of those UIDs alone, which needs `UIDPLUS`: a plain `EXPUNGE` would also remove whatever another
client had marked deleted, so on a server with neither, moving and deleting fail. A star
is `\Flagged`, set and cleared like `\Seen`.

Gmail's keys work — `e` archive, `#` delete, `!` spam, `s` star, `u` read or unread — except
while a field has the keys.

## Every action is a job, done by emguio whether or not the page is still open

An action is `POST /api/jobs`, answered as soon as it is queued. It takes a list, up to 500, and
queues all of it or none: one job that cannot be refuses the request. emguio then does the jobs of each
email config in the order asked, on a session for changes — whether or not the page that asked is
still open, and across a restart, since a waiting job is a row. A try that could not reach the
mail server is tried again, after five seconds, then thirty, two minutes and ten; an answer that
refused, or the last try, fails the job.

Jobs that follow one another and do the same to messages of one mailbox — what a selection asks
for — are done together, up to 500: one `UID FETCH` of their flags, which tells the messages
still there from those gone and says which were unread, then one `UID STORE` of those whose flag
changes, one `UID MOVE`, or one `UID STORE` of `\Deleted` and one `UID EXPUNGE`. A hundred
messages archived cost the mail server two commands rather than two hundred. Anything else asked
in between ends the run, so nothing is done out of order. A refusal or a lost connection is every
job's in it; a message another client took away first fails alone.

A newer read or star on a message replaces one still waiting on it, so read then unread pressed
quickly is one `STORE` of the flag it ends with. A move or a delete is never dropped: where a
message goes is not undone by what was asked after.

A failed job keeps its sentence until a page has shown it and let it go; one nobody comes back for
is swept after a day. `GET /api/jobs` lists what waits and what failed, so a page opened later —
a reload, another tab — draws them too.

The only thing a closed tab can lose is a job not yet taken: the page asks before closing while
one is on its way, which is the moment between a press and its answer. While any wait, a corner
of the screen says how many.

Open pages are told once a run of jobs is over rather than after each job: every telling has
them read INBOX's list again, page by page, and a selection of a hundred would be a hundred
reads while it is worked on, on the session the jobs are done on. While jobs wait, a page reads
`GET /api/jobs` every two seconds, so a long run shows how far it has got. The mirror's look
after moves is asked for once at the end of the run too.

## Every action is drawn before it is done, over what the server said

Read, star, archive, delete, spam and move change the screen on the press — the button, the
list's row, the folders' counts — and the job goes after. What is drawn is two things: what the
server last said, kept as it said it, and the jobs not yet done, on their way or waiting,
applied over it as it is drawn. A done job is written into the first in the step that stops it
being drawn as waiting, so it never shows undone in between.

So a failure has nothing to undo. The failed job stops being pending, and the screen is what the
server said, with every job asked after it still drawn; a pile of jobs with a failure at its end
loses only that one, and the notice says why. And a list read back while jobs wait is drawn with
them still applied, rather than showing the messages they were asked on as though nothing had
been done.

After a move or a delete the pane goes on to the message below it in the list, or else the one
above, or else back to the folder. Where the message is open in the list's place, on a phone, it
is always back to the folder. The mirror is asked for a look once the run is over: the
window has places to fill, and the counts are the server's to confirm.

## A selection is acted on in one request

The button beside the folder's name turns the list's rows into checkboxes; a Shift-click takes
every row from the last one clicked. The folder's header becomes the selection's: how many,
all or none, and the same actions as the reading pane, with the same keys. Escape, or the cross,
goes back to opening messages.

An action on a selection is one request, a job for each message, drawn like any other and done
together on the server.
Star and read follow the selection: a single unstarred or unread message makes the button star
or mark read, and only the messages that need it are asked about. Deleting from Trash, or on a
server with none, asks first how many go for good.

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

## INBOX as it changes, the counts every five minutes

A quick pass brings the window up to date; a full pass also lists the mailboxes and asks each
for its counts with `STATUS`, every five minutes. Fetch new mail, saving a config, and a run of
jobs that moved mail ask for a full pass now. A pass is said on the event stream when it moved
something, and always when it was asked for: the button turns until `synced_at` moves, so a
look that found nothing still ends. Mail that came in is lit in the list for a moment.

Between passes the worker waits in INBOX with `IDLE`, examined rather than selected, so waiting
there changes nothing. Whatever the server says unasked — mail arriving or leaving, a flag
another client changed — ends the wait with a quick pass, so new mail is in the list as it
arrives rather than up to a minute later. go-imap sends `IDLE` again before a server's
inactivity timeout. On a server without `IDLE`, INBOX is looked at every minute instead.

Mail that arrives while a pass is under way is said before anybody waits for it. So the wait,
opening INBOX, compares its count and `UIDNEXT` with what the pass found, and looks again when
they differ rather than waiting for the next message to say so.

The window is opened on every pass rather than only when `STATUS` has moved: a flag changed
elsewhere — a star — moves no number `STATUS` reports, and the newest 30 messages' flags cost
one short `FETCH`. A message that left the window, by being expunged or by newer mail pushing it
out, is dropped; one that entered it has its header fetched.

## Every folder as it changes, on a server with NOTIFY

On a server with `NOTIFY` (RFC 5465) — Dovecot has it — the worker asks, once per connection, to
be told about every mailbox in the personal namespace and not only the one it waits in:
`NOTIFY SET (SELECTED (MessageNew MessageExpunge FlagChange)) (PERSONAL (MessageNew MessageExpunge
FlagChange MailboxName))`. A mailbox that moves is then said with a `STATUS` in the middle of the
wait, and the worker asks that mailbox for its counts and nothing more. One made, renamed or
deleted is said with a `LIST`, and the mailboxes are listed again. Mail another client or a
server's filter puts in a folder is in the sidebar within a second, rather than at the next full
pass.

A change of flags elsewhere is asked for first, which some servers will not say without
CONDSTORE; refused, the worker asks for the rest, and refused again it waits as it would without
NOTIFY. A server that gives up saying (`NOTIFICATIONOVERFLOW`) is asked again, after a full pass
for what it stopped saying. The full pass every five minutes stays, for whatever a server did
not say. go-imap has NOTIFY only on its main branch so far, which emguio follows.

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

The next page is asked for while the end of the list is still 200px below what shows of it, so
scrolling down rarely reaches it. Until it arrives, the end is rows shaped like messages. A list
whose every row shown was moved away by a selection is not empty while the server has more: its
end comes into view, and the next page loads.

A next page asked for while the whole list is being read again waits for that read rather than
cutting it short, and is then asked for from what the read brought.

A list shows the server's internal date — when the message arrived. The `Date` header is the
sender's claim, which spam and misconfigured clients get wrong; it is kept beside it, as `sent`.

## A folder is searched on the mail server

Nothing is kept here to search, so a search is the server's `UID SEARCH` in the folder open,
INBOX included: `?q=` on the folder's list, paged like it, fifty at a time and newest first, each
run asking which matching UIDs are below the last shown. A server's own index — Gmail's,
Fastmail's, Dovecot's FTS — answers fast; one without searches every message and takes as long.
One folder at a time because `SEARCH` is: across folders would be a search of each.

What is typed is a few words. Every word has to be in the message, as `TEXT` — its headers or its
body; quotes keep words together. `from:`, `to:` and `subject:` narrow a word to that header,
`to:` taking Cc too; `is:unread`, `is:read` and `is:starred` are the flags. Anything else with a
colon, a link say, is a word. A query that is not ASCII goes with `CHARSET UTF-8`.

The search is in the address, `/c/ec_…/mb_…?q=…`, and stays there when a result is opened, so
the results stay beside it, and a reload or Back lands on them. The field sits above the list;
Enter searches, Escape or the cross goes back to the whole folder, and `/` reaches it from
anywhere but a field. Actions on results, one or a selection, are what they are anywhere.

Unread only, the envelope beside Select, is `is:unread` put in the query and taken out again: the
field shows the rest, and each stays as the other changes. A message read meanwhile leaves the
list when it is next read from the server.

## A conversation is the server's THREAD, with its other side from Sent

Nothing is kept here to thread by either, so conversations are the server's `UID THREAD
REFERENCES`: by References and In-Reply-To, then by subject for what has neither. Dovecot,
Cyrus — Fastmail — and most servers of their kind have it; Gmail does not, and there a message
stands on its own. `THREAD` answers for the whole folder at once, so its answer is kept in
memory, for the last eight folders asked about, while each holds what it held: the same
UIDVALIDITY, `UIDNEXT` and count.

The list stays a message a row. Once a page of it shows, it asks
`GET …/mailboxes/{mailbox}/threads?messages=…` how many messages each row's conversation holds
in the folder, and draws the number left of the date: the list never waits for it, the window
included. Opening a message asks for `GET …/messages/{message}/conversation`, shown under the
subject oldest first, with the open message marked. The rest open in place, as their text, and
from there in full; past six, the earlier ones wait to be asked for, and past a hundred in the
folder, the oldest are not shown. Drafts have none.

`THREAD` is one folder's, and one's own replies are in Sent. So a conversation is searched for
there too, with `UID SEARCH` by Message-ID: what answers one of its messages, in In-Reply-To or
References, and what one of them answers. For a message in Sent the other side is INBOX. A
message filed in both shows once. When a folder's counts move, its rows' numbers and every
conversation open are read again.

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
list. A list read from the server, a search, and the open message, are not refetched on every
event: each would be a trip to the mail server on every change. A list or a search is read again
once its folder's counts, read on that event, are not what they were — the server changed what
is in it, mail another client moved into Trash say, and the list would otherwise disagree with the
number beside its folder. Its `UIDNEXT` counts too: a draft saved again replaces one, and leaves
the numbers as they were. Fetch new mail asks for them again.

Events are at least a second apart. Changes closer together than that are one event, sent when
the second is up: INBOX's list past its window is read from the mail server, and a burst of
changes would be a burst of those reads.

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

On a session of its own, so opening a message never waits behind a pass, nor behind a long run of
jobs, which have another session for changes: jobs, drafts and filing in Sent. Each is opened on
demand, closed after five idle minutes, and serves one request at a time.

Opening is two requests: the headers and `BODYSTRUCTURE`, the server's description of every part
— its type, name, size and section — and then the text alone, its plain and HTML parts by
section. The list of attachments comes from the description; an attachment's bytes come only
when somebody downloads it, so a message with a large one opens as fast as one without.

Both are fetched with `BODY.PEEK` after checking that the mailbox's UIDVALIDITY is still the one
the message's id names. Neither changes anything; marking the message read is its own job.
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
