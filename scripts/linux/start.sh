#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "${BASH_SOURCE[0]}")/control-common.sh"

owned_file_if_present "$EU_BINARY"
[[ -f "$EU_BINARY" && -x "$EU_BINARY" ]] || die 'Missing executable: easyupdate'
owned_file_if_present "$EU_ROOT/config.yaml"
[[ -r "$EU_ROOT/config.yaml" ]] || die 'Missing readable config.yaml; no file was created or replaced.'

if read_pid_record; then
  if process_identity_matches "$EU_PID" "$EU_START"; then
    printf 'Already running, PID %s.\n' "$EU_PID"
    exit 0
  fi
  if kill -0 "$EU_PID" 2>/dev/null; then
    die 'PID belongs to a different or unverifiable process; refusing to start or stop it.'
  fi
  rm -- "$EU_PIDFILE"
fi

# A missing PID file must not allow a second copy to share this database.
for proc_path in /proc/[0-9]*; do
  candidate="${proc_path##*/}"
  candidate_start="$(process_start_token "$candidate")" || continue
  if process_identity_matches "$candidate" "$candidate_start"; then
    die "An untracked project process already exists (PID $candidate); refusing duplicate startup."
  fi
done

# Close the inherited lock descriptor in the background child.
nohup "$EU_BINARY" -config "$EU_ROOT/config.yaml" </dev/null >>"$EU_LOGFILE" 2>&1 9>&- &
EU_PID=$!
EU_START=""
for (( attempt=0; attempt<40; attempt++ )); do
  if ! kill -0 "$EU_PID" 2>/dev/null; then
    wait "$EU_PID" || true
    die 'Startup failed; inspect logs/server.log.'
  fi
  EU_START="$(process_start_token "$EU_PID")" || true
  if [[ -n "$EU_START" ]] && process_identity_matches "$EU_PID" "$EU_START"; then break; fi
  sleep 0.1
done
[[ -n "$EU_START" ]] && process_identity_matches "$EU_PID" "$EU_START" || die 'Cannot verify the started process; no PID record was written.'

# Noclobber also refuses unexpected new files created during startup.
if ! (set -o noclobber; printf '%s %s\n' "$EU_PID" "$EU_START" >"$EU_PIDFILE"); then
  if process_identity_matches "$EU_PID" "$EU_START"; then kill -TERM "$EU_PID"; fi
  die 'Could not create PID record; requested graceful shutdown of the verified new process.'
fi
sleep 2
if ! process_identity_matches "$EU_PID" "$EU_START"; then
  if ! kill -0 "$EU_PID" 2>/dev/null; then rm -- "$EU_PIDFILE"; fi
  die 'Process did not stay running; inspect logs/server.log.'
fi
printf 'Started PID %s. Check the configured HTTP address; log: %s\n' "$EU_PID" "$EU_LOGFILE"
