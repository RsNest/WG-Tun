#!/usr/bin/env bash
# Burst 20 valid HAProxy configs; expect one validation_passed and one reload_succeeded.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=smoke-common.sh
source "$ROOT/scripts/smoke-common.sh"

require_root
require_linux

if [ ! -f "$SMOKE_CFG" ]; then
  die "missing ${SMOKE_CFG}"
fi
if ! systemctl is-enabled proxyctl-haproxy-reload.path >/dev/null 2>&1; then
  die "enable proxyctl-haproxy-reload.path first (scripts/vm-bootstrap.sh)"
fi

mkdir -p -- "$SMOKE_DIR"
save_live_cfg
install_restore_trap

log "validating saved original config"
haproxy -c -f "${SMOKE_DIR}/haproxy.cfg.orig"

since=$(journal_since_now)
log "journal window starts at ${since}"
sleep 1

i=1
while [ "$i" -le 20 ]; do
  variant="${SMOKE_DIR}/variant-${i}.cfg"
  {
    printf '# proxyctl-smoke-debounce variant=%s ts=%s\n' "$i" "$(date -u +%Y%m%dT%H%M%S)"
    cat -- "${SMOKE_DIR}/haproxy.cfg.orig"
  } >"$variant"
  log "burst write ${i}/20"
  atomic_install_cfg "$variant"
  sleep 0.1
  i=$((i + 1))
done

log "waiting for debounce settle + reload (25s)"
sleep 25

dump="${SMOKE_DIR}/debounce.journal"
dump_journal_since "$since" "$dump"
log "journal excerpt written to ${dump}"

passed=$(count_event "$dump" "event=validation_passed")
reloaded=$(count_event "$dump" "event=reload_succeeded")
started=$(count_event "$dump" "event=reload_started")

if [ "$started" -gt 1 ]; then
  fail_with_journal "$dump" "debounce failed: reload_started=${started} (want <=1)"
fi
if [ "$passed" -ne 1 ]; then
  fail_with_journal "$dump" "debounce failed: validation_passed=${passed} (want 1)"
fi
if [ "$reloaded" -ne 1 ]; then
  fail_with_journal "$dump" "debounce failed: reload_succeeded=${reloaded} (want 1)"
fi
if [ "$started" -ne 1 ]; then
  fail_with_journal "$dump" "debounce failed: reload_started=${started} (want 1)"
fi

log "PASS debounce (one validation_passed, one reload_started, one reload_succeeded)"
