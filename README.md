# emguio

A self-hosted webmail client. Each user adds the mail servers they read from — an incoming
IMAP server and, optionally, an outgoing SMTP one — and reads their mail in the browser.

- **Self-hosted.** One static binary, one SQLite file, one port.
- **Invitation only.** A user exists because somebody with a shell on the host printed a link.
- **Email configs belong to the user.** Added in Settings rather than in a config file, and never
  mixed: one is shown at a time.

Early: users add their email configs and read their mail — folders, lists, messages with their attachments, HTML shown safely, remote images through a proxy that hides the reader and held back in Junk. Only INBOX's newest headers are kept; everything else is read from the server when it is asked for. Read, star, archive, delete, spam and move are done on the server.

## Run it

A key, once, kept somewhere other than the data volume: it seals the mail passwords users save,
and a new one makes every user enter theirs again.

```sh
openssl rand -base64 32
```

```sh
docker run -d --name emguio \
  -p 8080:80 \
  -v emguio-data:/data \
  -e EMGUIO_PUBLIC_URL=https://mail.example.com \
  -e EMGUIO_SECRET_KEY=<the key> \
  ghcr.io/reeywhaar/emguio:latest
```

With nobody signed up yet, the server logs an invitation link at startup — open it to make the
first user:

```sh
docker logs emguio
```

More users with `docker exec emguio emguio invite`. The environment, the image and running from a
checkout are in [docs/deploy.md](docs/deploy.md); how mail is read is in
[docs/reading.md](docs/reading.md); how the code is written is in
[docs/conventions.md](docs/conventions.md).
