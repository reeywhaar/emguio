# Email configs

An email config is one of a user's mail accounts: an incoming server to read from and,
optionally, an outgoing one to send through. A user adds them in Settings; the instance knows
nothing about mail until they do.

## An email config is its user's alone

Every query that touches one takes the user it belongs to, and another user's id answers
`404`, not `403`: whether an id exists is not somebody else's to learn.

The interface shows one at a time, chosen in the header, and nothing joins across them. A
unified inbox is a different product.

## Incoming is IMAP, and POP3 is in the schema already

`incoming_protocol` is a column, the form lists POP3, and the API refuses it with a sentence
saying it is not supported yet. When POP3 is written, storage does not change and nothing has
to be migrated: it is a branch in the code that reads mail, not a new shape of account.

## A server is secured with TLS or STARTTLS, never neither

There is no plain-text choice. STARTTLS fails the connection when the server does not offer it,
and an IMAP server that signs somebody in before STARTTLS is refused, so a password never
crosses the network in the clear because a server forgot to offer encryption.

## A password is sealed with the server key

emguio signs in on its users' behalf, so it has to be able to read their mail passwords back.
They are sealed with XChaCha20-Poly1305 under `EMGUIO_SECRET_KEY`, bound to the row and the
server they belong to: a sealed value copied into another row, or from incoming to outgoing,
does not open.

The key is never in the database and never in its backups. That is what it protects: a copied
`emguio.db`, or an archive of one, is not a list of mail passwords. It does not protect a running
host, which has to hold the key to dial anything.

**Changing the key makes every saved password unreadable.** Nothing breaks: the form says the
saved password can no longer be read and asks for it again. Keep the key where the backups are
not, and keep it.

A key per user, derived from their login password, was the stronger option and was not taken:
reading mail has to happen when nobody is signed in, and a server that can only decrypt during a
session cannot.

## A password is never sent back

No response carries one, including to the user who typed it. The form shows a saved password as
an empty field, and an empty password on an edit means "the one already saved".

## A saved password is sent only to the host it was saved for

An edit or a test that leaves the password empty reuses the saved one — unless the host has
changed, in which case it has to be typed again. Without that, whoever holds a session could
point the host at a server of their own, press Test, and collect the password. Typing it is
proof of knowing it, so a typed password goes wherever it is told.

## An outgoing server can share the incoming sign-in

The usual case, so it is the default: an outgoing server with no username of its own signs in
with the incoming username and password, and the password is stored once.

## emguio connects only to public addresses

Every host is typed by a user, so every dial is screened: loopback, private, link-local, shared
and reserved ranges are refused, including IPv4 addresses carried inside IPv6 translation
prefixes. The check runs on the address actually being connected to, after the name resolves,
so a name that answers differently the second time it is asked still cannot reach inside.

A mail server on the same network as emguio is reached by naming its range in
`EMGUIO_ALLOW_NETWORKS` — an operator's decision, made before the process starts.

## Testing a connection changes nothing

IMAP signs in, opens INBOX with `EXAMINE` and signs out. SMTP greets, signs in and quits without
sending. Each server is tested at once, and each test is bounded at fifteen seconds.

A server that refuses is a `200` with `ok: false` and its reason: the test ran, and that is its
answer. A draft that is wrong in itself — no host, POP3 — is a `400` like any other.

A test dials whatever it is given, so it is limited per user: a burst of ten, then one every six
seconds. Without that, a loop of tests would make an instance somebody's port scanner.

## What a server says is shown, made safe

A refusal carries the server's own words — "Invalid credentials", a link to the provider's help
— because they are usually the fastest way to the fix. They are cut to one line of two hundred
characters first, with anything that is not printable text replaced.
