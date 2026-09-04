#!/usr/bin/env bash
# proxyctl installer for Debian/Ubuntu. Does not overwrite existing HAProxy or
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

if ! id proxyctl >/dev/null 2>&1; then
  useradd --system --home /var/lib/proxyctl --shell /usr/sbin/nologin proxyctl
fi

install -d -m 0750 -o proxyctl -g proxyctl /var/lib/proxyctl /etc/proxyctl
install -d -m 0750 -o root -g root /run/proxyctl || true

HERE="$(cd "$(dirname "$0")/.." && pwd)"
if [ ! -x "$HERE/bin/proxyctl-controller" ] || [ ! -x "$HERE/bin/proxyctl-agent" ] || [ ! -x "$HERE/bin/proxctl" ]; then
  echo "build binaries first: go build -o bin/ ..." >&2
  exit 1
fi

install -m 0755 "$HERE/bin/proxctl" "$PREFIX/bin/proxctl"
if [ "$ROLE" = "controller" ]; then
  install -m 0755 "$HERE/bin/proxyctl-controller" "$PREFIX/bin/proxyctl-controller"
  CFG=/etc/proxyctl/controller.yaml
  if [ -e "$CFG" ]; then
    echo "refusing to overwrite existing $CFG" >&2
  else
    cat >"$CFG" <<EOF
listen: "${LISTEN}"
data_dir: "/var/lib/proxyctl"
tls:
  required: true
  cert_file: "/var/lib/proxyctl/certs/server.crt"
  key_file: "/var/lib/proxyctl/certs/server.key"
  auto_self_signed: true
auth:
  bootstrap_token_file: "/var/lib/proxyctl/bootstrap.token"
  hmac_required: true
  max_skew: 5m
rate_limit:
  mutating_rps: 10
  burst: 20
EOF
    chown proxyctl:proxyctl "$CFG"
    chmod 0640 "$CFG"
  fi
  install -m 0644 "$HERE/deploy/systemd/proxyctl-controller.service" /etc/systemd/system/proxyctl-controller.service
  systemctl daemon-reload
  systemctl enable --now proxyctl-controller
  sleep 1
  if ! curl -fsS -k "https://127.0.0.1:${LISTEN##*:}/healthz" >/dev/null 2>&1; then
    # listen may not be 127.0.0.1; try the configured listen host
    echo "controller started; verify /healthz on ${LISTEN}" >&2
  fi
  echo "controller installed. bootstrap token: /var/lib/proxyctl/bootstrap.token"
elif [ "$ROLE" = "agent" ]; then
  if [ -z "$CONTROLLER" ] || [ -z "$TOKEN" ] || [ -z "$NODE_NAME" ]; then
    echo "agent role requires --controller --token --node-name" >&2
    exit 1
  fi
  install -m 0755 "$HERE/bin/proxyctl-agent" "$PREFIX/bin/proxyctl-agent"
  CFG=/etc/proxyctl/agent.yaml
  TOK=/etc/proxyctl/agent.token
  if [ -e "$CFG" ]; then
    echo "refusing to overwrite existing $CFG" >&2
  else
    cat >"$CFG" <<EOF
node_name: "${NODE_NAME}"
controller_url: "${CONTROLLER}"
token_file: "${TOK}"
reconcile_interval: 10s
dry_run_only: false
state_dir: /run/proxyctl
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
  # live agent, install deploy/systemd/proxyctl-haproxy-reload.path instead.
  install -m 0644 "$HERE/deploy/systemd/proxyctl-agent.service" /etc/systemd/system/proxyctl-agent.service
  systemctl daemon-reload
  systemctl enable --now proxyctl-agent
  sleep 1
  curl -fsS "http://127.0.0.1:9101/healthz" >/dev/null
  echo "agent installed for node ${NODE_NAME}"
else
  echo "unknown role $ROLE" >&2
  exit 1
fi
