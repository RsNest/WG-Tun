#!/usr/bin/env bash
# transitforge installer for Debian/Ubuntu. Does not overwrite existing HAProxy or
# firewall configuration.
set -euo pipefail

ROLE=""
LISTEN="127.0.0.1:8443"
CONTROLLER=""
TOKEN=""
NODE_NAME=""
PREFIX="/usr/local"

usage() {
  cat <<EOF
Usage:
  $0 --role controller [--listen HOST:PORT]
  $0 --role agent --controller URL --token TOKEN --node-name NAME
EOF
  exit 1
}

while [ $# -gt 0 ]; do
  case "$1" in
    --role) ROLE="${2:-}"; shift 2 ;;
    --listen) LISTEN="${2:-}"; shift 2 ;;
    --controller) CONTROLLER="${2:-}"; shift 2 ;;
    --token) TOKEN="${2:-}"; shift 2 ;;
    --node-name) NODE_NAME="${2:-}"; shift 2 ;;
    -h|--help) usage ;;
    *) echo "unknown argument: $1" >&2; usage ;;
  esac
done

if [ -z "$ROLE" ]; then
  usage
fi

if [ ! -f /etc/os-release ]; then
  echo "cannot detect OS" >&2
  exit 1
fi
# shellcheck disable=SC1091
. /etc/os-release
case "${ID:-}" in
  debian|ubuntu) ;;
  *)
    echo "unsupported distro: ${ID:-unknown} (Debian/Ubuntu required)" >&2
    exit 1
    ;;
esac

need_pkg() {
  dpkg -s "$1" >/dev/null 2>&1 || {
    echo "missing package: $1 (install it, then re-run)" >&2
    return 1
  }
}

missing=0
need_pkg ca-certificates || missing=1
if [ "$ROLE" = "agent" ]; then
  need_pkg iproute2 || missing=1
  need_pkg iptables || missing=1
fi
if [ "$missing" -ne 0 ]; then
  exit 1
fi

if ! id transitforge >/dev/null 2>&1; then
  useradd --system --home /var/lib/transitforge --shell /usr/sbin/nologin transitforge
fi

install -d -m 0750 -o transitforge -g transitforge /var/lib/transitforge /etc/transitforge
install -d -m 0750 -o root -g root /run/transitforge || true

HERE="$(cd "$(dirname "$0")/.." && pwd)"
if [ ! -x "$HERE/bin/transitforge-controller" ] || [ ! -x "$HERE/bin/transitforge-agent" ] || [ ! -x "$HERE/bin/transitforge" ]; then
  echo "build binaries first: go build -o bin/ ..." >&2
  exit 1
fi

install -m 0755 "$HERE/bin/transitforge" "$PREFIX/bin/transitforge"
if [ "$ROLE" = "controller" ]; then
  install -m 0755 "$HERE/bin/transitforge-controller" "$PREFIX/bin/transitforge-controller"
  CFG=/etc/transitforge/controller.yaml
  if [ -e "$CFG" ]; then
    echo "refusing to overwrite existing $CFG" >&2
  else
    cat >"$CFG" <<EOF
listen: "${LISTEN}"
data_dir: "/var/lib/transitforge"
tls:
  required: true
  cert_file: "/var/lib/transitforge/certs/server.crt"
  key_file: "/var/lib/transitforge/certs/server.key"
  auto_self_signed: true
auth:
  bootstrap_token_file: "/var/lib/transitforge/bootstrap.token"
  hmac_required: true
  max_skew: 5m
rate_limit:
  mutating_rps: 10
  burst: 20
EOF
    chown transitforge:transitforge "$CFG"
    chmod 0640 "$CFG"
  fi
  install -m 0644 "$HERE/deploy/systemd/transitforge-controller.service" /etc/systemd/system/transitforge-controller.service
  systemctl daemon-reload
  systemctl enable --now transitforge-controller
  sleep 1
  if ! curl -fsS -k "https://127.0.0.1:${LISTEN##*:}/healthz" >/dev/null 2>&1; then
    # listen may not be 127.0.0.1; try the configured listen host
    echo "controller started; verify /healthz on ${LISTEN}" >&2
  fi
  echo "controller installed. bootstrap token: /var/lib/transitforge/bootstrap.token"
elif [ "$ROLE" = "agent" ]; then
  if [ -z "$CONTROLLER" ] || [ -z "$TOKEN" ] || [ -z "$NODE_NAME" ]; then
    echo "agent role requires --controller --token --node-name" >&2
    exit 1
  fi
  install -m 0755 "$HERE/bin/transitforge-agent" "$PREFIX/bin/transitforge-agent"
  CFG=/etc/transitforge/agent.yaml
  TOK=/etc/transitforge/agent.token
  if [ -e "$CFG" ]; then
    echo "refusing to overwrite existing $CFG" >&2
  else
    cat >"$CFG" <<EOF
node_name: "${NODE_NAME}"
controller_url: "${CONTROLLER}"
token_file: "${TOK}"
reconcile_interval: 10s
dry_run_only: false
state_dir: /run/transitforge
haproxy_config: /etc/haproxy/haproxy.cfg
haproxy_reload: systemctl
metrics_listen: "127.0.0.1:9101"
tls:
  insecure_skip_verify: false
EOF
    chmod 0640 "$CFG"
  fi
  if [ -e "$TOK" ]; then
    echo "refusing to overwrite existing $TOK" >&2
  else
    umask 077
    printf '%s\n' "$TOKEN" >"$TOK"
    chmod 0600 "$TOK"
  fi
  if [ -e /etc/haproxy/haproxy.cfg ]; then
    echo "existing HAProxy config left untouched: /etc/haproxy/haproxy.cfg"
  fi
  # Native agent reloads HAProxy itself (haproxy_reload: systemctl). For a Docker
  # live agent, install deploy/systemd/transitforge-haproxy-reload.path instead.
  install -m 0644 "$HERE/deploy/systemd/transitforge-agent.service" /etc/systemd/system/transitforge-agent.service
  systemctl daemon-reload
  systemctl enable --now transitforge-agent
  sleep 1
  curl -fsS "http://127.0.0.1:9101/healthz" >/dev/null
  echo "agent installed for node ${NODE_NAME}"
else
  echo "unknown role $ROLE" >&2
  exit 1
fi
