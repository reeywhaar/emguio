# Deploy

One image, one port, one directory.

```
ghcr.io/reeywhaar/emguio:latest
```

## Environment

| variable | default | meaning |
| --- | --- | --- |
| `EMGUIO_PUBLIC_URL` | *required* | The address you open in a browser |
| `EMGUIO_DATA_DIR` | `/data` | Where `emguio.db` lives. Fixed in the image; a variable for local runs |
| `EMGUIO_ALLOW_NETWORKS` | — | Private addresses or ranges emguio may connect to, comma-separated |
| `EMGUIO_TRUSTED_PROXIES` | `private` | Whose `X-Forwarded-For` is believed; see [The caller](#the-caller-is-the-first-x-forwarded-for-hop-that-is-not-a-trusted-proxy) |
| `EMGUIO_BACKUP_URL` | — | A backup agent's `POST` address; see [Backups](#backups). Unset, nothing is backed up |
| `EMGUIO_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`; see [Logs](#logs) |

**There is no config file.** What an operator adjusts once the process is running belongs in
the database, where it is a form field rather than a redeploy. The environment is for what must
be true before the process starts.

**Mail servers are not configured here at all.** Each user adds their own email configs in
Settings, and the instance knows nothing about mail until they do.

**The web directory is a constant and the data directory is a variable.** Both are fixed at one
path inside the image. The difference is what happens when the path is wrong: a missing bundle
is the placeholder page, so a checkout that never builds the frontend runs against a `/srv/web`
that does not exist — while a missing data directory refuses to start, which would make a bare
`go run .` impossible without somewhere to point it. The variable exists for that, and for
tests.

## `EMGUIO_PUBLIC_URL`

Required, validated at startup, and never inferred from a request. `Host` and
`X-Forwarded-Host` are both client-supplied, so an invitation link built from one is an
invitation link a stranger controls.

It decides three things: whether the session cookie carries `Secure`, what invitation links
say, and who notifications say they are from. Notifications need it to be https: a browser
registers a service worker only on https or on `localhost`. Every device that turned them on is
bound to this address, and has to turn them on again after a move — see [push.md](push.md).

A trailing slash is trimmed. A path is refused — emguio serves from the root, and a prefix would
produce links that half work.

## There is no server key

Each user's mail passwords are sealed with a key their seal key opens, which they type after each
start of the instance; nothing in the environment opens them. See
[email-configs.md](email-configs.md). An `EMGUIO_SECRET_KEY` left from before is not read, and
emguio says so in its log at start. Each user from before signs in and gives that key once, which
keeps their saved passwords and is their seal key from then on; then the variable can go.

## `EMGUIO_ALLOW_NETWORKS`

emguio connects only to public addresses, because every host it dials was typed by a user. A
mail server on the same network as emguio is named here, as addresses or CIDR ranges:

```sh
EMGUIO_ALLOW_NETWORKS=10.0.0.0/8,192.168.1.20
```

## The caller is the first `X-Forwarded-For` hop that is not a trusted proxy

emguio sits behind a proxy, so the connection it sees is the proxy's. Each proxy appends who it
accepted to `X-Forwarded-For`, so emguio reads the header right to left, stepping over hops that
are trusted proxies; the first one that is not is the caller. Everything left of it is whatever
the caller sent.

The header is read at all only when the connection comes from a trusted proxy. From anywhere else
it is the caller's own claim, and believing it would let a stranger choose the address fail2ban
bans — yours, say. `X-Real-IP` is never read: a proxy that does not set it passes the caller's
along.

`EMGUIO_TRUSTED_PROXIES` names the trusted proxies: addresses, ranges, `private`, or `none`,
separated by commas or spaces.

| setup | value |
| --- | --- |
| Proxy on the same host or Docker network | `private`, the default |
| Proxy on another machine | its address, e.g. `198.51.100.3` |
| A CDN in front of that proxy | `private` plus the CDN's published ranges |
| No proxy, or the port is reachable without it | `none` |

Caddy and Traefik set the header by default; nginx needs
`proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for`.

`private` is 10/8, 172.16/12, 192.168/16, fc00::/7 and loopback. It assumes every caller with
such an address is yours — on a shared Docker network, every other container; where that is not
so, name the proxy itself instead.

## The data directory

```sh
docker run -d --name emguio \
  -e EMGUIO_PUBLIC_URL=https://mail.example.com \
  -v emguio-data:/data \
  -p 8080:80 \
  ghcr.io/reeywhaar/emguio:latest
```

**The image declares no `VOLUME`, and a missing data directory refuses to start.**

A `VOLUME` directive makes docker invent an anonymous volume whenever nobody mounts anything.
That litters the host with orphaned volumes nobody can name, and hides the mistake: the
container runs, the database is written somewhere, and it disappears the next time the container
is replaced.

So there is no directive, and `serve` checks the directory exists before it opens anything. A
forgotten `-v` is a startup error naming the flag.

**emguio never creates the directory.** Mounting it is the operator's statement about where the
data lives, and inventing one would be emguio guessing at that.

## Backups

Set `EMGUIO_BACKUP_URL` to a backup agent and emguio posts it a gzipped `VACUUM INTO` snapshot
of the database. Leave it unset and emguio takes no backups at all. The snapshot is written to a
`backup-*` folder in the data directory and removed once sent; one left by a process killed
mid-pass is removed on the next start.

```
emguio ──POST archive──▶ agent ──▶ wherever the agent was told
       (no credential)    (holds the token, names the file, prunes old ones)
```

**emguio holds no credential, no provider, no bucket and no retention policy.** It decides when
to back up and what goes in the archive; everything after that belongs to the agent. A
compromised emguio cannot read, overwrite or delete a single existing backup: it has nothing to
authenticate with and nothing to point at. It can only hand over one more archive.

```yaml
services:
  emguio:
    image: ghcr.io/reeywhaar/emguio:latest
    environment:
      EMGUIO_PUBLIC_URL: https://mail.example.com
      EMGUIO_BACKUP_URL: http://backup:8080/backup
    volumes: [emguio-data:/data]
    ports: ["8080:80"]

  backup:
    # The agent holds the token and decides where archives end up.
    image: ghcr.io/reeywhaar/backio-agent:latest
    environment:
      # …destination and credential, which are the agent's business

volumes:
  emguio-data:
```

### A copy goes out when somebody chose something, and at no other time

The loop looks every half hour and sends only when something a person chose was written since
the last archive the agent took: a user or an invitation, a mail account or anything it is set
to, a folder's notifications or order, a device, the instance's push keys, the notification and
reading settings. A restart sends one, because a process that has just started may be on a
volume nobody has a copy of yet.

Most of what emguio writes does not count. The mail it keeps is a copy of the mail servers', read
again after a restore; jobs are done and gone; a draft is on the server's Drafts within seconds;
a sign-in is the process noticing somebody. Counting those would send an archive every half hour
for as long as anybody's mail arrives.

There is no heartbeat: an instance nobody has changed sends nothing, and whether it is running is
`/healthz`'s to say.

### Restoring

Stop the container, extract the archive into the data directory, start it again. Everybody stays
signed in, because sessions are in the same file; each user unseals with their seal key, as after
any start, and their mail is read again from the servers.

The archive holds every password hash and every device's subscription, and the mail passwords
sealed with keys only their users' seal keys open: worth the same care as the directory itself.

## Logs

One JSON object a line, on stderr, so a collector reads each field rather than parsing a
sentence. A line says what happened in words, and its fields say whose it was — `user`,
`email_config`, the mail server's `host` — and, when something did not go through, `why` in the
words a user is shown, the `error` as it came, and what happens next, such as `retry_in`.
Durations are written as they read, `"1.2s"`.

At `info`: every request with its status and how long it took; a list read from the mail server
that took two seconds or more, with how long each step took — waiting for the connection, signing
in, opening the folder, ordering it, searching, the headers and the previews; mail sync connecting,
being up to date, failing and stopping; each mail action and each message sent; accounts, folders
and settings lookups as they change; and the sweeps. At `debug`, also each routine look at the mail server.

Never in a line: a password, an email address, a subject, a folder's name, what somebody searched
for, or a request's query. A folder is named by its id and its use — `sent`, `drafts` or `own`.

### A wrong password is one line, for fail2ban

emguio bans nobody itself. Each refused sign-in is logged with the caller's address, and never the
username:

```
{"time":"…","level":"INFO","msg":"sign-in refused: the username and password do not match","ip":"203.0.113.7"}
```

```ini
# filter.d/emguio.local
[Definition]
failregex = ^\{.*"msg":"sign-in refused.*"ip":"<ADDR>"
ignoreregex =
```

## Port

`:80` inside the container, not configurable. Remap it with `-p`.

## Getting in

There is no sign-up page and no default password. A user exists because somebody with a shell on
the host printed an invitation:

```sh
docker exec emguio emguio invite
```

The link is good for a week and makes one user, with a seal key they choose. Accepting it signs
them in. After every start of the container each user unseals with that key before their mail is
read; see [email-configs.md](email-configs.md).

On an empty database `serve` logs one at startup, as the `link` of its line, so the first user
needs nothing but the container's log:

```sh
docker logs emguio
```

Every user is equal. There is no administrator, because there is nothing yet for one to
administer that the command line does not already cover.

## The image

Multi-stage, and the stages do not depend on each other.

- **Go stage** cross-compiles: `CGO_ENABLED=0`, `-trimpath`, version stamped through
  `-ldflags`. `modernc.org/sqlite` is pure Go, which is what keeps this static.
- **Node stage** builds the bundle. It arrives with `web/`.
- **`COPY` path by path**, not a `.dockerignore`. An allowlist cannot accidentally admit
  `web/node_modules` or a local `data/`.
- **Alpine runtime with `ca-certificates`** — mail servers are reached over TLS, and their
  certificates are checked against the system's roots.
- **`HEALTHCHECK` runs `emguio healthcheck`**, a second process asking the first, so the image
  needs no HTTP client and a wedged server fails it.

## A dependency's missing fix is a patch, applied at build

go-smtp's release cannot ask a server to hold a message (FUTURERELEASE, RFC 4865), so emguio
builds against it with
[patches/github.com/emersion/go-smtp+v0.25.1.patch](../patches/github.com/emersion/go-smtp+v0.25.1.patch)
applied. `sh patch-packages.sh` does that for every patch in `patches/`, as npm's patch-package
does: it fetches the release, applies the patch to a copy in `.patched/`, which git ignores, and
points `go.mod` at the copy. The Dockerfile and CI run it; run it once after cloning, and again
when a patch changes. A patch is a diff rather than a copy of the module, so what was changed
stays readable, and it goes once a release has it.

## Tags

A push to `main` that passes the tests moves two tags onto one image:

| tag | what it means |
| --- | --- |
| `:latest` | whatever was published last. Fine for a first look, and a pin to nothing |
| `:v1` | this line of the software, fixes included. What a deployment pins to |

A rollback names an earlier image by its digest, `ghcr.io/reeywhaar/emguio@sha256:…`.

What the binary says it is, is the commit it was built from: stamped through `-ldflags`, printed
by `emguio version`, and on the startup line.

## The bundle is read from disk, not embedded

`serve` hands the static server an `os.DirFS` over `/srv/web`, walked once at startup.

Embedding would make every stylesheet an input to the Go compiler, so a one-line CSS change
would invalidate the layer that compiles the binary. Reading from disk is what lets the two
image stages be independent, and what lets `go test ./...` pass with no frontend build present.

## The API reference is built with the bundle

`/docs` is `docs/api.md` rendered to HTML by the web build, `node scripts/docs.mjs`, into the
bundle beside the app as `docs.html` and `docs.md`; `docs/` is a build input for that. Static,
the whole text in the page, and served to anybody, signed in or not: a program does one GET and
reads it. `/docs.md` is the markdown, and `/llms.txt` points at it. The examples are written
against `https://emguio.example.com`, which the server replaces with `EMGUIO_PUBLIC_URL`.

The image also carries `docs/` at `/srv/docs`, so a checkout with no bundle still serves the
markdown. A test holds the reference to the route table, both ways, and to the error codes the
API sends.

A missing bundle is the placeholder page rather than a failure.

## CI

`test` → `publish` → `notify`, on push to `main` and by `workflow_dispatch`.

Publishing on every push is safe because `publish` needs `test`: a red build skips it rather
than shipping a broken image as `latest`. `publish` pushes `:latest` and `:v1`, then smoke-tests
the image by the digest it pushed: health, a JSON 404, the sign-in shell, and an invitation
accepted all the way to the application shell.

`notify` reports through notifio when `NOTIFIO_HOST` and `NOTIFIO_TOKEN` are set as repository
secrets, and stays quiet when they are not.

## Running from a checkout

```sh
sh patch-packages.sh
cd web && npm ci && npm run build && cd ..
mkdir -p data
EMGUIO_PUBLIC_URL=http://localhost EMGUIO_DATA_DIR=data go run . serve
```

`npm run dev` in `web/` serves the frontend with reloading and sends `/api` to the Go server on
port 80.
