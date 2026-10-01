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
| `EMGUIO_SECRET_KEY` | *required* | 32 random bytes, base64. Seals the passwords of email configs |
| `EMGUIO_ALLOW_NETWORKS` | — | Private addresses or ranges emguio may connect to, comma-separated |
| `EMGUIO_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

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

It decides two things: whether the session cookie carries `Secure`, and what invitation links
say.

A trailing slash is trimmed. A path is refused — emguio serves from the root, and a prefix would
produce links that half work.

## `EMGUIO_SECRET_KEY`

Required. It seals the password of every email config, and emguio refuses to start without it:

```sh
openssl rand -base64 32
```

**Keep it apart from the data directory and its backups**, which is the point of it: a copied
database is not a list of mail passwords. **And keep it**: a new key makes every saved password
unreadable, and each user has to enter theirs again. See [email-configs.md](email-configs.md).

## `EMGUIO_ALLOW_NETWORKS`

emguio connects only to public addresses, because every host it dials was typed by a user. A
mail server on the same network as emguio is named here, as addresses or CIDR ranges:

```sh
EMGUIO_ALLOW_NETWORKS=10.0.0.0/8,192.168.1.20
```

## The data directory

```sh
docker run -d --name emguio \
  -e EMGUIO_PUBLIC_URL=https://mail.example.com \
  -e EMGUIO_SECRET_KEY=… \
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

## Port

`:80` inside the container, not configurable. Remap it with `-p`.

## Getting in

There is no sign-up page and no default password. A user exists because somebody with a shell on
the host printed an invitation:

```sh
docker exec emguio emguio invite
```

The link is good for a week and makes one user. Accepting it signs them in.

On an empty database `serve` prints one at startup, so the first user needs nothing but the
container's log:

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
cd web && npm ci && npm run build && cd ..
mkdir -p data
EMGUIO_PUBLIC_URL=http://localhost EMGUIO_DATA_DIR=data \
  EMGUIO_SECRET_KEY=<the same key every run> go run . serve
```

`npm run dev` in `web/` serves the frontend with reloading and sends `/api` to the Go server on
port 80.
