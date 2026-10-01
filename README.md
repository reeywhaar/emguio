# emguio

A self-hosted webmail client. Each user adds the mail servers they read from — an incoming
IMAP server and, optionally, an outgoing SMTP one — and reads their mail in the browser.

- **Self-hosted.** One static binary, one SQLite file, one port.
- **Invitation only.** A user exists because somebody with a shell on the host printed a link.
- **Email configs belong to the user.** Added in Settings rather than in a config file, and never
  mixed: one is shown at a time.

Early: signing in works, mail does not yet.

## Run it

```sh
docker run -d --name emguio \
  -p 8080:80 \
  -v emguio-data:/data \
  -e EMGUIO_PUBLIC_URL=https://mail.example.com \
  ghcr.io/reeywhaar/emguio:latest
```

With nobody signed up yet, the server logs an invitation link at startup — open it to make the
first user:

```sh
docker logs emguio
```

More with `docker exec emguio emguio invite`. The environment, the image and running from a
checkout are in [docs/deploy.md](docs/deploy.md); how the code is written is in
[docs/conventions.md](docs/conventions.md).
