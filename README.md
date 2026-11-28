# emguio

A self-hosted webmail client. Each user adds the mail servers they read from — an incoming
IMAP server and, optionally, an outgoing SMTP one — and reads their mail in the browser.

- **Self-hosted.** One static binary, one SQLite file, one port.
- **Invitation only.** A user exists because somebody with a shell on the host printed a link.
- **Email configs belong to the user.** Added in Settings rather than in a config file, and never
  mixed: one is shown at a time.
- **Sealed with each user's own key.** Mail passwords open only with the seal key their user types
  after each restart; neither the host nor a copy of the database reads them.
- **Your mail goes where you take it.** Move it between your accounts, export it to a file, and
  import it from one, all in the browser.

## What it does

**Reading**

- Folders with their unread counts, and lists newest first by the date mail arrived.
- Messages open with their attachments listed. HTML is shown in a sandbox, and remote images load
  through a proxy that hides the reader; in Junk they wait until asked for.
- A folder is searched on the mail server, and Unread only narrows any list.
- A message shows the conversation it is in, where the server threads, with the replies from Sent.
- Newsletters are tagged. Unsubscribe stops one in a click where its sender allows that, and
  otherwise opens their page.

**Keeping it tidy**

- Read, star, archive, delete, spam and move, for one message or a selection, done on the mail
  server. Gmail's keys work, and with a mouse, mail is dragged onto a folder to move it there.
- Trash and Junk empty in one go. From any folder's menu, all of it is marked read, or its mail
  older than 30 days deleted.
- Folders of one's own are made, renamed, moved, deleted with their mail after asking, and dragged
  into order. The server's own stay as they are.

**Taking your mail with you**

- Move mail to another of your accounts, at another provider too. Each message is copied across
  with its flags and the date it arrived, then deleted here. A card in the corner shows how far it
  has got and can cancel the rest. If it stops partway, no message is lost.
- Export a folder with its subfolders, or a selection, as a zip. Inside are mbox files, which every
  mail app imports and which keep read and flagged, or eml files, one per message. The zip is
  written as the mail is read, so a large folder takes no more memory than a small one.
- After the download, delete exactly what went into the file, and nothing that arrived since. To
  leave a provider: export, check the file, delete.
- Import an mbox, an eml, or a zip of them, from emguio or another mail app. Folders in the zip
  become folders here. It runs in the background, and mail a folder already has is skipped, so an
  import that stopped can simply be run again.

**Writing**

- New, reply, reply all and forward, sent through the account's outgoing server and kept in Drafts
  while being written.
- Send later, where the outgoing server can hold mail (FUTURERELEASE), as mailio can. Where the
  server shows held mail in a Scheduled folder, it can be changed or cancelled until it goes.

**Accounts**

- Any number of mail accounts, added in Settings, where the address is often enough to find the
  servers. They are dragged into the order the user wants.
- The header shows which accounts have unread mail, and the tab's icon gets a dot.
- An account can be turned off without deleting it: it is not read or notified from until it is
  turned on again.
- Settings lists where the user is signed in, each with its browser and address, and signs any of
  them out, or all but this one. A new password, given the current one, signs out all but this one
  too.

**Web app and notifications**

- Runs as a web app: installed from the browser on a phone or a computer, it opens in its own
  window like any other app.
- Sends notifications of new mail to the devices a user chooses. Each account sends them right
  away or as a digest, every 30 minutes to 4 hours, from the folders ticked.
- Notifications can come without sound, and not at all during quiet hours: what arrives then is
  told in one notification per account when they end.

**Small**

- Only each folder's newest headers are kept, Inbox's 30 and every other's 10, so a folder opens at
  once. Everything else is read from the mail server when asked for. Message bodies and attachments
  are not stored, except a draft's while it is being written.
- Dates are ISO, like 2026-11-13, and times are on a 24-hour clock.
- Backed up, where a backup agent is set: a snapshot of the database goes to it whenever something
  was chosen in it. See [docs/deploy.md](docs/deploy.md#backups).

## Install

### 1. Compose it

Behind [caddy-docker-proxy](https://github.com/lucaslorentz/caddy-docker-proxy), which gives it a
certificate from its labels and reaches it over the external `caddy` network:

```yaml
# compose.yml
services:
  emguio:
    image: ghcr.io/reeywhaar/emguio:latest
    restart: unless-stopped
    environment:
      EMGUIO_PUBLIC_URL: https://mail.example.com
    volumes:
      - emguio-data:/data
    networks:
      - caddy
    labels:
      caddy: mail.example.com
      caddy.reverse_proxy: "{{upstreams 80}}"

volumes:
  emguio-data:

networks:
  caddy:
    external: true
```

### 2. Start it

```sh
docker compose up -d
docker compose logs emguio
```

With nobody signed up yet, it logs an invitation for the first user:

```
emguio-1  | {"time":"…","level":"INFO","msg":"no users yet: open this link to make the first one","link":"https://mail.example.com/invite/r8Kp…","expires_at":"…"}
```

### 3. Make your user

Open the link, and choose a username, a password and a seal key. The seal key opens your saved
mail passwords: emguio asks for it after every restart, and nothing reads your mail until you give
it. Keep it somewhere safe; a forgotten one can be replaced, and each mail account then asks for
its password again.

[![Making a user](docs/screenshots/invite.png)](docs/screenshots/invite.png)

### 4. Add a mail account

In **Settings → Add mail account**, the address is often enough: the servers are filled in from
what the provider publishes, then the password, and **Test connection** signs in to both before
anything is saved.

[![Adding a mail account](docs/screenshots/account.png)](docs/screenshots/account.png)

### 5. Read your mail

[![The mail](docs/screenshots/mail.png)](docs/screenshots/mail.png)

On a phone:

<a href="docs/screenshots/phone.png"><img src="docs/screenshots/phone.png" width="320" alt="The mail on a phone"></a>

Invented mail, captured from the real thing by `npm --prefix web run screenshots`; see
[docs/screenshots](docs/screenshots/).

More users with `docker compose exec emguio emguio invite`.

## Try it

`./run_demo.sh` runs emguio in Docker with a mail server of its own and five accounts of invented
mail, the mail fresh on every start; then sign in at <http://localhost:8080>. See [demo/](demo/).

## More

- [docs/deploy.md](docs/deploy.md): the environment, the image, and running from a checkout.
- [docs/api.md](docs/api.md): the HTTP API, also served at `/docs` on any instance.
- [docs/email-configs.md](docs/email-configs.md): mail accounts, their servers and passwords.
- [docs/reading.md](docs/reading.md): how mail is read.
- [docs/sending.md](docs/sending.md): how mail is written and sent.
- [docs/push.md](docs/push.md): the app and its notifications.
- [docs/conventions.md](docs/conventions.md): how the code is written.
