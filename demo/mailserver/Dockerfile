# syntax=docker/dockerfile:1

# The demo's mail server, built from this module as the main image is: go.mod points some modules
# at patched copies, made here.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY patch-packages.sh ./
COPY patches ./patches
RUN apk add --no-cache patch && sh patch-packages.sh
RUN go mod download
COPY demo/mailserver ./demo/mailserver
RUN CGO_ENABLED=0 go build -trimpath -o /out/mailserver ./demo/mailserver

FROM alpine:3
COPY --from=build /out/mailserver /usr/local/bin/mailserver
EXPOSE 993 465 443
ENTRYPOINT ["mailserver"]
