# Sending mail

A message is written in a window over whatever is open — a new one, a reply, a reply to all, a
forward — and sent through the email config's outgoing server. While it is written emguio keeps
it, and puts it in the config's Drafts on its mail server every so often; once its window is
closed and it is there, or it is sent, nothing of it is kept here.

## A message is sent while its writer waits, and nothing is queued

`POST /api/email-configs/{id}/send` answers once the outgoing server has taken the message, or
with the sentence it refused it in: a recipient it will not deliver to, a password it will not
take. The window stays open on a refusal, with what was written, to be sent again. A server
that took it is the moment it has gone; there is no outbox to sit in, and nothing to undo.

The request's own context does not bound the sending: a tab closed while the server is still
taking a large attachment leaves the sending to finish. Two minutes bound it instead.

A config with no outgoing server has nothing to send through, and the window says so, with the
way to its settings.

## It goes from the config's address, under its sender's name

`From` is the config's email address with the name its settings give — "Your name", in the
outgoing server's box. Without one it is the address alone. The SMTP envelope's sender is the
address.

## Attachments travel in the request, base64

The API takes JSON and nothing else, which is half of what keeps another site from posting to it,
so files come as base64 in the body: up to 25 MB of them, which most mail servers will not take
more than, in a request of up to 36 MB. A file goes with the first save of a draft, and emguio
holds it from then on.

## A reply is threaded, and its original marked answered

A reply names the message it answers. The server reads that message's `Message-ID` and
`References` and sends `In-Reply-To` and `References` from them, so every client that threads
puts the reply under it. A message gone from the server since it was opened is answered
unthreaded rather than not at all. Once sent, the original is given `\Answered`.

Who a reply goes to is worked out in the window, where it can still be changed: where the sender
asks replies to go (`Reply-To`), else the sender; to all, everybody else it went to as well, Cc
staying Cc; never the writer's own address. One's own message, in Sent say, is replied to its
recipients. The subject gains `Re:` once, however many replies it has been through in other
clients — `RE:`, `AW:`, `SV:` count. The quote is the message's text, or its HTML as text when it
has none, each line under `>`, with the cursor above it.

## A forward carries the original's attachments

A forward's text is the original's below a line saying it was forwarded and from whom, and its
attachments go with it: the window lists them, any can be taken off, and the server fetches each
from the mail server by its section as it builds the message. The images an HTML message shows
in place are not attachments, and are not carried.

## A copy is filed in Sent unless the server filed one

Once the outgoing server has the message, the writer is told, and in the background, three
seconds later, emguio looks in the config's Sent folder for the message's `Message-ID`. Gmail
files what its SMTP is given; so do others, and a second copy there would be one too many. Only
when it is not there is the message appended, read. A config with no Sent folder keeps no copy.
What fails here is logged: the message has already gone.

## A draft is kept here, and put in Drafts every so often

Two seconds after writing pauses, the window saves to emguio: `POST /api/email-configs/{id}/drafts`
the first time, `PUT .../drafts/{draft}` after, taking what `send` takes. A save is a row here
and nothing on the mail server, so it is cheap and answered at once, however often it comes. The
first reads what the draft answers and the attachments it carries, from the mail server, once;
the answer names the attachments emguio holds, and later saves name those to keep, with only new
files after them. Nothing is uploaded or fetched twice.

emguio writes the draft to the config's Drafts folder on its own: thirty seconds after a save, and
at most that often while somebody types, so the mail server — and every other client watching
Drafts — sees a new copy now and then rather than at every pause. A closed tab or a browser that
crashed loses at most the last two seconds, and the draft still reaches Drafts. Writing appends
the draft, read and `\Draft`, then deletes the copy it replaces with `\Deleted` and `UID EXPUNGE`:
the new one first, so a failure leaves the old. One draft at a time, and on a session for changes,
so opening a message never waits behind one. A write the server would not take is tried again,
after thirty seconds, two minutes, ten and then hourly.

That needs UIDPLUS: `APPENDUID` says where the new copy went, and `UID EXPUNGE` removes the old
one alone. A config without a Drafts folder has nowhere to put one, and is refused the first
save; one without UIDPLUS finds out at the first write. Either way closing asks before discarding
what is written, as it would with nowhere to keep it.

A draft holds what a message to send would not: its Bcc in a header, to be read back; a field that
is not addresses yet, as typed; nobody to send to at all.

Closing writes the draft to Drafts now, and it goes from here; the window says so. A mail server
that cannot be reached leaves the draft here, written once it can be, and the window says that
instead. Discard deletes it here, and its copy in Drafts as a delete job, so it leaves the list at
once. Sending names the draft: it is not written while the message goes out, and once it has gone
it goes from here and its copy from Drafts, along with the copy filed in Sent. A draft nobody has
saved for a week — a window left open, or one whose server would not take it — is swept.

In Drafts a message opens with Edit draft rather than Reply and Forward: its fields, text and
attachments back in the window, kept here again at the first save and written in place of itself.
A reply opened again is threaded as it was, from the draft's own `In-Reply-To` and `References`;
its original is not marked answered, since only the window that began the reply knew where that
is.
