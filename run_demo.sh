#!/bin/sh
# Runs the demo: emguio with a mail server of its own and five accounts of invented mail. The
# mail is made fresh on every run; emguio's user, its sign-in and its accounts are kept, so a
# browser signed in stays signed in. Ctrl-C stops it. See demo/README.md.
#
#   ./run_demo.sh              on http://localhost:8080
#   ./run_demo.sh --fresh      without what earlier runs kept: signed out, accounts added anew
#   DEMO_PORT=9090 ./run_demo.sh
set -eu

cd "$(dirname "$0")/demo"
port="${DEMO_PORT:-8080}"

logs=""
stop() {
  trap - EXIT INT TERM
  [ -n "$logs" ] && kill "$logs" 2>/dev/null
  echo
  echo "Stopping the demo…"
  docker compose down --remove-orphans >/dev/null 2>&1 || true
}
trap stop EXIT INT TERM

# Anything left from a run that was not stopped goes first, and with --fresh what runs kept too.
if [ "${1:-}" = "--fresh" ]; then
  docker compose down --volumes --remove-orphans >/dev/null 2>&1 || true
else
  docker compose down --remove-orphans >/dev/null 2>&1 || true
fi
docker compose up --build --detach

echo "Filling the accounts…"
if ! docker compose wait seed >/dev/null; then
  docker compose logs seed
  echo "The demo could not be set up." >&2
  exit 1
fi

echo
echo "Ready: http://localhost:$port"
echo "Sign in as demo, password demodemo; the seal key, if asked, is demodemodemo. Ctrl-C stops the demo."
echo
# In the background and waited for, so a signal ends the wait at once and the demo goes down.
docker compose logs --follow --no-log-prefix emguio &
logs=$!
wait "$logs"
