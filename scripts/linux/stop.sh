#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "${BASH_SOURCE[0]}")/control-common.sh"

if ! read_pid_record; then
  printf '%s\n' 'No project PID record; nothing was signaled.'
  exit 0
fi
if ! kill -0 "$EU_PID" 2>/dev/null; then
  rm -- "$EU_PIDFILE"
  printf '%s\n' 'Removed stale project PID record; nothing was signaled.'
  exit 0
fi
process_identity_matches "$EU_PID" "$EU_START" || die 'PID identity does not match executable, cwd and start time; nothing was signaled.'
kill -TERM "$EU_PID"

# The application has a 15-second graceful HTTP shutdown timeout.
for (( attempt=0; attempt<100; attempt++ )); do
  if ! kill -0 "$EU_PID" 2>/dev/null || ! process_identity_matches "$EU_PID" "$EU_START"; then
    rm -- "$EU_PIDFILE"
    printf 'Stopped verified project PID %s.\n' "$EU_PID"
    exit 0
  fi
  sleep 0.2
done
die 'Process still exists after 20 seconds; PID record retained. No SIGKILL was sent.'
