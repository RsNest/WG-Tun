#!/usr/bin/env bash
# Invalid haproxy.cfg must restore last-good without a reload loop.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=smoke-common.sh
source "$ROOT/scripts/smoke-common.sh"

require_root
require_linux

if [ ! -f "$SMOKE_CFG" ]; then
  die "missing ${SMOKE_CFG}"
fi
if [ ! -f "$HAPROXY_LAST_GOOD" ]; then
  die "missing last-good ${HAPROXY_LAST_GOOD}; seed with: systemctl start ${SMOKE_UNIT}"
fi
if ! systemctl is-enabled proxyctl-haproxy-reload.path >/dev/null 2>&1; then
  die "enable proxyctl-haproxy-reload.path first (scripts/vm-bootstrap.sh)"
fi

mkdir -p -- "$SMOKE_DIR"
save_live_cfg
install_restore_trap

want_hash=$(sha256_file "${SMOKE_DIR}/haproxy.cfg.orig")
good_hash=$(sha256_file "$HAPROXY_LAST_GOOD")
if [ "$want_hash" != "$good_hash" ]; then
  log "live cfg hash differs from last-good; using live copy as expected restore target"
fi

since=$(journal_since_now)
log "journal window starts at ${since}"
sleep 1

bad="${SMOKE_DIR}/invalid.cfg"
log "writing deliberately invalid ${SMOKE_CFG}"
printf 'this is not haproxy\n' >"$bad"
atomic_install_cfg "$bad"

log "waiting for debounce + restore + follow-up skip (15s)"
sleep 15

dump="${SMOKE_DIR}/restore.journal"
dump_journal_since "$since" "$dump"
log "journal excerpt written to ${dump}"

failed=$(count_event "$dump" "event=validation_failed")
restored=$(count_event "$dump" "event=restore_on_failure_triggered")
skipped=$(count_event "$dump" "event=reload_skipped reason=unchanged_hash")
started=$(count_event "$dump" "event=reload_started")

if [ "$failed" -lt 1 ]; then
  fail_with_journal "$dump" "restore failed: no validation_failed"
fi
if [ "$restored" -lt 1 ]; then
  fail_with_journal "$dump" "restore failed: no restore_on_failure_triggered"
fi
if [ "$skipped" -ne 1 ]; then
  fail_with_journal "$dump" "restore failed: reload_skipped reason=unchanged_hash count=${skipped} (want 1)"
fi
if [ "$started" -ne 0 ]; then
  fail_with_journal "$dump" "restore failed: reload_started=${started} (want 0; restore must not reload)"
fi

# Order: validation_failed before restore before skip.
awk_script='
  /event=validation_failed/ && !f { f=NR }
  /event=restore_on_failure_triggered/ && !r { r=NR }
  /event=reload_skipped reason=unchanged_hash/ && !s { s=NR }
  END {
    if (f==0 || r==0 || s==0) { exit 2 }
    if (!(f<r && r<s)) { exit 3 }
  }
'
if ! awk "$awk_script" "$dump"; then
  fail_with_journal "$dump" "restore failed: expected sequence validation_failed -> restore_on_failure_triggered -> reload_skipped"
fi

got_hash=$(sha256_file "$SMOKE_CFG")
if [ "$got_hash" != "$want_hash" ] && [ "$got_hash" != "$good_hash" ]; then
  fail_with_journal "$dump" "live cfg hash ${got_hash} matches neither original ${want_hash} nor last-good ${good_hash}"
fi

log "PASS restore (failed -> restore -> one skip; live hash matches last-good/original)"
