#!/usr/bin/env bash
# Orchestrate VM bootstrap + HAProxy reload smoke tests on a disposable Linux host.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=smoke-common.sh
source "$ROOT/scripts/smoke-common.sh"

require_root
require_linux

rm -f -- "$SMOKE_STAMP"

log "running vm-bootstrap.sh"
"$ROOT/scripts/vm-bootstrap.sh"

log "seeding last-good via one oneshot reload"
systemctl start "$SMOKE_UNIT"
ok=0
i=1
while [ "$i" -le 30 ]; do
  if [ -f "$HAPROXY_LAST_GOOD" ]; then
    ok=1
    break
  fi
  sleep 1
  i=$((i + 1))
done
if [ "$ok" -ne 1 ]; then
  journalctl -u "$SMOKE_UNIT" --no-pager -n 80 >&2 || true
  die "last-good was not created after ${SMOKE_UNIT}"
fi

debounce_rc=0
restore_rc=0

log "running smoke-test-debounce.sh"
if "$ROOT/scripts/smoke-test-debounce.sh"; then
  log "RESULT debounce=PASS"
else
  debounce_rc=$?
  log "RESULT debounce=FAIL rc=${debounce_rc}"
fi

log "waiting 15s for path-unit follow-up from debounce restore trap"
sleep 15

log "running smoke-test-restore.sh"
if "$ROOT/scripts/smoke-test-restore.sh"; then
  log "RESULT restore=PASS"
else
  restore_rc=$?
  log "RESULT restore=FAIL rc=${restore_rc}"
fi

printf '\n===== smoke summary =====\n'
if [ "$debounce_rc" -eq 0 ]; then
  printf 'debounce: PASS\n'
else
  printf 'debounce: FAIL\n'
fi
if [ "$restore_rc" -eq 0 ]; then
  printf 'restore:  PASS\n'
else
  printf 'restore:  FAIL\n'
fi

if [ "$debounce_rc" -ne 0 ] || [ "$restore_rc" -ne 0 ]; then
  die "one or more smoke tests failed"
fi

mkdir -p -- /var/lib/proxyctl
printf 'passed_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$SMOKE_STAMP"
log "wrote ${SMOKE_STAMP}"
log "next: scripts/enable-live-overlay.sh (dry_run_only remains true)"
