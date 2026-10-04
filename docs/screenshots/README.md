# Screenshots

The pictures in the project README, captured from a real emguio reading invented mail.

```sh
npm --prefix web run screenshots
```

That builds the frontend and a Linux binary, and starts in Docker a Dovecot holding
[`seed.mjs`](../../web/scripts/screenshots/seed.mjs)'s mail as `imap.example.com`, a stand-in that answers as
`smtp.example.com` and publishes both as `autoconfig.example.com`, and emguio on an empty data
directory, with certificates made for the run and nothing outside resolving. Then it walks the
install in headless Chromium over the DevTools protocol — the invitation, the mail account, the
mail — and overwrites the PNGs here. Everything it starts is stopped on the way out. Each PNG says
its density, 72 dpi times `SCALE`, as a macOS screenshot does.

emguio runs in a container even on a Mac: a binary built for macOS trusts only the system's
certificates, and these are made up a moment before.

Needs `go`, `node`, `docker`, `openssl` and Chromium or Chrome.

**A run with nothing changed changes nothing.** The mail is dated from a fixed moment and the
page's clock starts there, in UTC and `en-US`, so the dates drawn are the same on any day; the
caret is hidden and animations finished. A new shot that differs from the file already there in
under a thousandth of its pixels, by at most 24 levels, leaves the file alone.

| knob | |
| --- | --- |
| `SCALE=2` | device pixels per CSS pixel |
| `THEME=dark` | the other theme; `OUT=/tmp/shots` keeps it off the committed set |
| `ONLY=mail` | just those shots; the steps before them still run, unphotographed |
| `NOTES=0` | without the notes |
| `TIMEOUT=30` | seconds a shot may take; one that runs over says what it was waiting for |

**The data** is `seed.mjs`: the user, the mail account, the folders, and the messages, each with
the folder it is in, how long ago it came, its flags, the one it answers, and its attachments.
Mail is appended oldest first, since a list is in the order mail arrived.

**The shots** are the list at the bottom of
[`capture.mjs`](../../web/scripts/screenshots/capture.mjs), in the install's order: where to go,
what to do there, optionally one element to crop to, and what to do after, which runs whether or
not the shot is taken. Each can carry notes: small popovers drawn over the page, pointing at what
they explain.
