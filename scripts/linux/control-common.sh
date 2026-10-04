#!/usr/bin/env bash
# Project-local controls for the deployment at this exact path.
set -euo pipefail
export LC_ALL=C
umask 077

EU_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
if [[ "$EU_ROOT" != /home/zlight106/easyupdate ]]; then
  printf '%s\n' 'Refusing to run outside /home/zlight106/easyupdate.' >&2
  exit 1
fi
cd -- "$EU_ROOT"
EU_UID="$(id -u)"
EU_BINARY="$EU_ROOT/easyupdate"
EU_PIDFILE="$EU_ROOT/run/easyupdate.pid"
EU_LOGFILE="$EU_ROOT/logs/server.log"

die() { printf '%s\n' "$*" >&2; exit 1; }

owned_directory() {
  local path="$1"
  [[ ! -L "$path" ]] || die "Refusing symlink directory: $path"
  [[ -d "$path" ]] || die "Not a directory: $path"
  [[ "$(stat -c %u -- "$path")" == "$EU_UID" ]] || die "Directory is owned by another user: $path"
}

owned_file_if_present() {
  local path="$1"
  [[ ! -L "$path" ]] || die "Refusing symlink file: $path"
  if [[ -e "$path" ]]; then
    [[ -f "$path" ]] || die "Not a regular file: $path"
    [[ "$(stat -c %u -- "$path")" == "$EU_UID" ]] || die "File is owned by another user: $path"
    [[ "$(stat -c %h -- "$path")" == 1 ]] || die "Refusing multiply-linked file: $path"
  fi
}

owned_directory "$EU_ROOT"
command -v flock >/dev/null || die 'flock is required.'
for path in "$EU_ROOT/run" "$EU_ROOT/logs"; do
  [[ ! -L "$path" ]] || die "Refusing symlink directory: $path"
  if [[ ! -e "$path" ]]; then mkdir -m 700 -- "$path"; fi
  owned_directory "$path"
done
owned_file_if_present "$EU_ROOT/run/control.lock"
owned_file_if_present "$EU_PIDFILE"
owned_file_if_present "$EU_LOGFILE"
exec 9>>"$EU_ROOT/run/control.lock"
flock -n 9 || die 'Another project control operation is running.'

# /proc stat field 22 is a kernel start-time token. Strip the comm field,
# which can contain spaces and parentheses, before indexing the remainder.
process_start_token() {
  local line
  local -a fields
  line="$(cat -- "/proc/$1/stat" 2>/dev/null)" || return 1
  read -r -a fields <<<"${line##*) }"
  [[ ${#fields[@]} -ge 20 && "${fields[19]}" =~ ^[0-9]+$ ]] || return 1
  printf '%s' "${fields[19]}"
}

process_identity_matches() {
  local pid="$1" expected_start="$2" exe cwd actual_start owner
  [[ "$pid" =~ ^[0-9]+$ && "$expected_start" =~ ^[0-9]+$ ]] || return 1
  [[ ${#pid} -le 10 ]] || return 1
  (( 10#$pid > 1 )) || return 1
  kill -0 "$pid" 2>/dev/null || return 1
  owner="$(stat -c %u -- "/proc/$pid" 2>/dev/null)" || return 1
  [[ "$owner" == "$EU_UID" ]] || return 1
  exe="$(readlink -- "/proc/$pid/exe" 2>/dev/null)" || return 1
  # An atomic replacement can leave a running copy marked '(deleted)'.
  [[ "$exe" == "$EU_BINARY" || "$exe" == "$EU_BINARY (deleted)" ]] || return 1
  cwd="$(readlink -- "/proc/$pid/cwd" 2>/dev/null)" || return 1
  [[ "$cwd" == "$EU_ROOT" ]] || return 1
  actual_start="$(process_start_token "$pid")" || return 1
  [[ "$actual_start" == "$expected_start" ]]
}

read_pid_record() {
  local extra
  owned_file_if_present "$EU_PIDFILE"
  [[ -f "$EU_PIDFILE" ]] || return 1
  [[ "$(wc -l <"$EU_PIDFILE")" == 1 ]] || die 'Unexpected PID record format; refusing to signal anything.'
  read -r EU_PID EU_START extra <"$EU_PIDFILE" || die 'Cannot read PID record.'
  [[ "$EU_PID" =~ ^[0-9]+$ && "$EU_START" =~ ^[0-9]+$ && -z "$extra" ]] || die 'Invalid PID record; refusing to signal anything.'
  [[ ${#EU_PID} -le 10 ]] || die 'Invalid PID range.'
  (( 10#$EU_PID > 1 )) || die 'Invalid PID range.'
}
