# Notifications

emguio is an app a phone or a computer can install from the browser, and it tells the devices a
user lets it about their new mail: each message as it arrives, or gathered into a digest, as each
mail account is set to.

emguio does not deliver a notification. It hands it, encrypted, to the push service the browser
named — Apple's, Google's, Mozilla's — and that service wakes the device. Three RFCs:
[8030](https://www.rfc-editor.org/rfc/rfc8030) for the protocol,
[8291](https://www.rfc-editor.org/rfc/rfc8291) for the encryption,
[8292](https://www.rfc-editor.org/rfc/rfc8292) for naming the sender. All of it is
`internal/webpush`, on the standard library.

## Web push is written here, against the RFC's own example

`crypto/ecdh`, `crypto/hkdf`, `crypto/aes` and `crypto/ecdsa` are everything it needs. A push that
does not arrive looks exactly like a push nobody sent, so the part that fails silently is the
part to be able to read whole. `TestSealMatchesRFC8291` encrypts RFC 8291's worked example with
its keys and salt and compares the bytes: a wrong derivation round-trips with itself and fails
only at a real push service, as a `400` that reads like a malformed request.

The VAPID signature is `r ‖ s`, 64 bytes, not the DER `ecdsa.SignASN1` makes, which every push
service refuses. The token's audience is the push service's origin, not the endpoint, and its
subject is `EMGUIO_PUBLIC_URL`, which RFC 8292 allows.

## The keypair is made once and kept in the database

A P-256 keypair per instance, made on first start, in the `vapid` table. Every subscription is
bound to it: losing it ends them all, and no browser can be made to renew against a new one, so
it is never rotated and it travels with the database's backups.

## The origin is part of every subscription

A subscription belongs to the origin that made it. An instance moved to another address keeps
its database and loses every device: each browser has to turn notifications on again.

A browser registers a service worker only on https, or on `localhost`. `serve` warns at startup
when `EMGUIO_PUBLIC_URL` is http.

## A device is a browser, named by itself

A device is a browser profile that turned notifications on, holding one subscription. Browsers
renew a subscription on their own, and the old endpoint goes on taking messages for a while, so a
browser keyed by its endpoint shows every notification twice after a renewal. The page keeps an
id of its own in localStorage — per profile, as a subscription is — and a registration with the
same id replaces the row it had. A page looked at never makes one: only turning notifications on
does.

The same endpoint registered by another user passes to them: a browser signed in as somebody else
since tells them now, not the one before.

The page hands its subscription over every time it opens. Not every browser tells its worker
about a renewal, and this is what keeps a device from going quiet without anybody knowing. So a
device removed from another one is told again once it opens emguio; turned off on itself, it is
not.

Signing out turns this device off first. A phone signed out of shows nobody's mail on its lock
screen.

A push service answering `404` or `410` deletes the device; it is not coming back. A `429` or a
`5xx` keeps it, with the reason, which Settings shows.

An endpoint is an address a browser handed over and emguio posts to, so it is https, and dialed
through the screen like a mail server.

## New mail is what came in since the last look

A message is news when it is past the UIDs its folder had reached, unread, and dated by the server
since the folder was last looked at, less an hour for a server's clock. Mail filed in from
another folder — by a rule, by hand, by another client — keeps the date it first arrived on, and
is not news. Nothing is news on a folder's first look, nor after its UIDVALIDITY changes.

Each one is kept, in `arrivals`, only while its account notifies, its folder does, and its user
has a device to tell, so turning notifications on never brings a backlog with it.

## A message is told once, whichever folders it is in

Gmail files one message in INBOX and under each of its labels at once. An arrival whose
Message-ID is already kept for the account is not kept again, and arrivals are remembered for a
week, so a message filed into a second folder later is known too.

## INBOX and the user's own folders notify until chosen

A folder notifies or not as its user chose; until then INBOX and the folders the user made do,
and the server's others — Sent, Drafts, Archive, All Mail, Junk, Trash — do not. Junk would tell
of spam, and All Mail of every message twice.

## A digest waits its interval from the first message it holds

An account notifies `now`, as a `digest` every 30, 60, 120 or 240 minutes, or not at all.

With `now`, each message is told as soon as emguio sees it arrive: in INBOX within seconds on a
server with IDLE, in another folder as it changes on a server with NOTIFY, or else at the next
look. More than three at once are one notification counting them, not a stack.

A digest is due when the oldest message waiting has waited its interval. Nothing waits longer
than the interval, and two digests are never closer than it. A day's quiet costs nothing: the
first message after it starts the clock.

What was read before a notification goes out is left out of it, and so is a message no longer
in its folder or in a folder that stopped notifying. Turning an account off drops what was
waiting.

An arrival is marked told before it is sent: a notification lost to a crash is better than one
told twice.

## What a notification says

One message: who it is from, its subject and the start of its text; a tap opens it. Several:
how many, and who and what for the first five; a tap opens their folder, or the account when
they are in more than one. A user with more than one account is told which.

Each account's notifications share a `Topic`, so a phone that was off hears, of each account,
only the newest, for up to a day. A digest carries the account as its tag, so a newer one takes
the older's place on screen; a message carries its own.

WebKit does not replace a notification by its tag
([bug 258922](https://bugs.webkit.org/show_bug.cgi?id=258922)), so the worker closes any with the
same tag before it shows another.

## What the push service learns

What a notification says is encrypted to keys only the browser holds: the push service sees a
length and a time. It does learn when somebody has mail, which no encryption hides.

## The worker is a file of its own at the root

`web/public/sw.js` is plain JavaScript outside the bundle. A worker's scope is its directory,
and its address has to stay the same from one build to the next for the browser to find its new
version, which a content-hashed name would not. It is served `no-cache`, like the manifest.

A tap while emguio is open brings its window forward and moves it to the message, without
loading the page again.

## On iPhone and iPad, from the Home Screen

Safari shows web notifications only for an app added to the Home Screen; in a tab, PushManager is
absent. Settings says how to add it there rather than offering a button that cannot work. What
the browser can do decides, never its user agent; the user agent only chooses which sentence to
show.
