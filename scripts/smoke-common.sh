#!/usr/bin/env bash
# Shared helpers for live-overlay smoke tests. Sourced, not executed.
# shellcheck shell=bash

SMOKE_CFG="${SMOKE_CFG:-/etc/haproxy/haproxy.cfg}"
SMOKE_UNIT="${SMOKE_UNIT:-transitforge-haproxy-reload.service}"
SMOKE_DIR="${SMOKE_DIR:-/run/transitforge/smoke}"
SMOKE_STAMP="${SMOKE_STAMP:-/var/lib/transitforge/smoke-tests.passed}"
HAPROXY_LAST_GOOD="${HAPROXY_LAST_GOOD:-/var/lib/transitforge/haproxy.last-good.cfg}"

log() {
  printf 'transitforge-smoke: %s\n' "$*" >&2
}

die() {
  printf 'transitforge-smoke: FAIL %s\n' "$*" >&2
  exit 1
}

require_root() {
  if [ "$(id -u)" -ne 0 ]; then
    die "run as root"
  fi
}

require_linux() {
  if [ "$(uname -s)" != "Linux" ]; then
    die "Linux host required"
  fi
}

sha256_file() {
  local out
  out=$(sha256sum -- "$1") || return 1
  printf '%s\n' "${out%% *}"
}

atomic_install_cfg() {
  local src="$1"
  local tmp="${SMOKE_CFG}.transitforge-smoke-tmp"
  log "writing ${SMOKE_CFG} from ${src}"
  cp -a -- "$src" "$tmp"
  mv -f -- "$tmp" "$SMOKE_CFG"
}

save_live_cfg() {
  mkdir -p -- "$SMOKE_DIR"
  log "saving live ${SMOKE_CFG} aside"
  cp -a -- "$SMOKE_CFG" "${SMOKE_DIR}/haproxy.cfg.orig"
}

restore_live_cfg() {
  if [ -f "${SMOKE_DIR}/haproxy.cfg.orig" ]; then
    log "restoring original ${SMOKE_CFG}"
    atomic_install_cfg "${SMOKE_DIR}/haproxy.cfg.orig"
  fi
}

install_restore_trap() {
  restore_live_cfg_on_exit() {
    restore_live_cfg
  }
  trap restore_live_cfg_on_exit EXIT INT TERM
}

journal_since_now() {
  date -u +'%Y-%m-%d %H:%M:%S UTC'
}

dump_journal_since() {
  local since="$1"
  local dest="$2"
  journalctl -u "$SMOKE_UNIT" --since "$since" --no-pager -o short-iso >"$dest" || true
}

count_event() {
  local logf="$1"
  local needle="$2"
  grep -F -c "$needle" "$logf" 2>/dev/null || true
}

fail_with_journal() {
  local logf="$1"
  shift
  printf 'transitforge-smoke: FAIL %s\n' "$*" >&2
  printf 'transitforge-smoke: offending journal (%s):\n' "$logf" >&2
  cat -- "$logf" >&2 || true
  exit 1
}
