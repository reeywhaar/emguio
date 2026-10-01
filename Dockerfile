# syntax=docker/dockerfile:1

# Built on the machine doing the building rather than under emulation: the bundle is
# architecture-independent, so there is no reason to run npm through QEMU.
FROM --platform=$BUILDPLATFORM node:26-alpine AS web
WORKDIR /src
# The lockfile first, on its own layer, so a source edit does not reinstall the world.
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/tsconfig.json web/vite.config.ts web/vitest.config.ts ./
# Each entry named: an allowlist cannot accidentally admit web/node_modules or a local data/.
COPY web/index.html web/login.html ./
COPY web/public ./public
COPY web/src ./src
RUN npm run build
# An empty bundle is otherwise invisible until somebody loads the page and gets the
# placeholder, which looks like a server problem rather than a build one.
RUN test -s dist/index.html && test -s dist/login.html && test -s dist/favicon.svg

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
COPY internal ./internal
ARG TARGETOS TARGETARCH VERSION=dev
# CGO off keeps this a static binary and the runtime image free of a toolchain.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X emguio/internal/app.Version=$VERSION" -o /out/emguio .

FROM alpine:latest
# Mail servers are reached over TLS.
RUN apk add --no-cache ca-certificates
COPY --from=build /out/emguio /usr/local/bin/emguio
COPY --from=web /src/dist /srv/web
EXPOSE 80
# Runs the binary's own subcommand, so the image needs no HTTP client.
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s CMD ["emguio", "healthcheck"]
ENTRYPOINT ["emguio"]
CMD ["serve"]
