#!/bin/sh
# Runs the demo: emguio with a mail server of its own and five accounts of invented mail, made
# fresh. Ctrl-C stops it and throws all of it away. See demo/README.md.
#
#   ./run_demo.sh              on http://localhost:8080
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
  docker compose down --volumes --remove-orphans >/dev/null 2>&1 || true
}
trap stop EXIT INT TERM

# Anything left from a run that was not stopped goes first, so this one starts empty.
docker compose down --volumes --remove-orphans >/dev/null 2>&1 || true
docker compose up --build --detach

echo "Filling the accounts…"
if ! docker compose wait seed >/dev/null; then
  docker compose logs seed
  echo "The demo could not be set up." >&2
  exit 1
fi

echo
echo "Ready: http://localhost:$port"
echo "Sign in as demo, password demodemo. Ctrl-C stops the demo and throws it away."
echo
# In the background and waited for, so a signal ends the wait at once and the demo goes down.
docker compose logs --follow --no-log-prefix emguio &
logs=$!
wait "$logs"
