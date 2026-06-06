#!/bin/sh
set -eu

node server.js &
NEXT_PID=$!

./daily-speaking-api &
API_PID=$!

term() {
  kill "$NEXT_PID" "$API_PID" 2>/dev/null || true
}
trap term INT TERM EXIT

while kill -0 "$NEXT_PID" 2>/dev/null && kill -0 "$API_PID" 2>/dev/null; do
  sleep 1
done

term
wait "$NEXT_PID" 2>/dev/null || true
wait "$API_PID" 2>/dev/null || true
exit 1
