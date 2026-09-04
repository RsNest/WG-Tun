#!/usr/bin/env bash
# Prepare a throwaway Debian/Ubuntu VM for transitforge live-overlay smoke tests.
# Does not start the live agent. Idempotent.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=smoke-common.sh
source "$ROOT/scripts/smoke-common.sh"

require_root
require_linux

if [ ! -f /etc/os-release ]; then
  die "cannot detect OS"
fi
# shellcheck disable=SC1091
. /etc/os-release
case "${ID:-}" in
debian | ubuntu) ;;
*)
  die "unsupported distro: ${ID:-unknown} (Debian/Ubuntu required)"
  ;;
esac

export DEBIAN_FRONTEND=noninteractive

log "installing packages (docker, haproxy, wireguard-tools, curl, socat, jq)"
apt-get update -qq
pkgs=(ca-certificates curl jq haproxy wireguard-tools socat iptables iproute2)
apt-get install -y --no-install-recommends "${pkgs[@]}"

if ! command -v docker >/dev/null 2>&1; then
  log "installing docker.io"
  apt-get install -y --no-install-recommends docker.io
fi

if ! docker compose version >/dev/null 2>&1; then
  log "installing docker compose plugin"
  apt-get install -y --no-install-recommends docker-compose-v2 || apt-get install -y --no-install-recommends docker-compose-plugin || true
fi
if ! docker compose version >/dev/null 2>&1; then
  die "docker compose plugin missing after install (need docker-compose-v2)"
fi

log "enabling docker and haproxy"
systemctl enable --now docker
systemctl enable --now haproxy

if [ ! -f "$SMOKE_CFG" ]; then
  die "haproxy config missing: ${SMOKE_CFG}"
fi
log "validating existing ${SMOKE_CFG}"
haproxy -c -f "$SMOKE_CFG"

log "installing transitforge HAProxy reload units (path unit only enabled)"
install -d -m 0755 /usr/local/lib/transitforge /var/lib/transitforge /run/transitforge /etc/transitforge
install -m 0755 "$ROOT/scripts/haproxy-reload-on-change.sh" /usr/local/lib/transitforge/haproxy-reload-on-change.sh
install -m 0644 "$ROOT/deploy/systemd/transitforge-haproxy-reload.path" /etc/systemd/system/transitforge-haproxy-reload.path
install -m 0644 "$ROOT/deploy/systemd/transitforge-haproxy-reload.service" /etc/systemd/system/transitforge-haproxy-reload.service
systemctl daemon-reload
systemctl enable --now transitforge-haproxy-reload.path
systemctl disable transitforge-haproxy-reload.service >/dev/null 2>&1 || true

log "not starting live agent (compose overlay is a later step)"

if ! lsmod | grep -q '^wireguard'; then
  log "loading wireguard kernel module (ok if this VM has no module)"
  modprobe wireguard || log "modprobe wireguard failed (continuing)"
fi

log "vm-bootstrap complete"
