# Sending mail

A message is written in a window over whatever is open — a new one, a reply, a reply to all, a
forward — and sent through the email config's outgoing server. Nothing of it is kept here, before
it is sent or after.

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
more than, in a request of up to 36 MB. Nothing is uploaded ahead, so nothing waits on the server
for a message that is never sent.

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
