#!/usr/bin/env bash
# Start the live overlay only after smoke tests passed, with an agent-role
# token and dry_run_only: true. Does not flip dry_run_only to false.
#
# Source build (default): docker compose --build
# Released images: TRANSITFORGE_VERSION=sha-<short> sudo -E bash scripts/enable-live-overlay.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=smoke-common.sh
source "$ROOT/scripts/smoke-common.sh"

require_root
require_linux

LIVE_YAML="$ROOT/configs/docker-agent.live.yaml"
TOKEN_HOST=/etc/transitforge/agent.token
TOKEN_NAME="${TRANSITFORGE_AGENT_TOKEN_NAME:-ru-edge-1-agent}"

if [ ! -f "$SMOKE_STAMP" ]; then
  die "smoke tests have not passed (${SMOKE_STAMP} missing). Run scripts/run-smoke-tests.sh first."
fi
if [ ! -f "$LIVE_YAML" ]; then
  die "missing ${LIVE_YAML}"
fi
if ! command -v docker >/dev/null 2>&1; then
  die "docker not installed"
fi
if ! docker compose version >/dev/null 2>&1; then
  die "docker compose plugin missing"
fi
if ! command -v jq >/dev/null 2>&1; then
  die "jq required (scripts/vm-bootstrap.sh installs it)"
fi

cd "$ROOT"

COMPOSE_FILES=(-f docker-compose.yml)
BUILD_OPTS=(--build)
if [ -n "${TRANSITFORGE_VERSION:-}" ]; then
  export TRANSITFORGE_VERSION
  COMPOSE_FILES+=(-f docker-compose.release.yml)
  BUILD_OPTS=()
  log "using GHCR images TRANSITFORGE_VERSION=${TRANSITFORGE_VERSION} (no local --build)"
fi

compose() {
  docker compose "${COMPOSE_FILES[@]}" "$@"
}

# Live overlay after the base file; put the release overlay last so GHCR
# image: and build reset win if live.yml ever sets image/build.
compose_live() {
  if [ -n "${TRANSITFORGE_VERSION:-}" ]; then
    docker compose -f docker-compose.yml -f docker-compose.live.yml -f docker-compose.release.yml "$@"
  else
    docker compose -f docker-compose.yml -f docker-compose.live.yml "$@"
  fi
}

json_field() {
  jq -r --arg k "$1" '.[$k] // empty'
}

log "rewriting ${LIVE_YAML} to token_file=/etc/transitforge/agent.token and dry_run_only=true"
tmp="${LIVE_YAML}.tmp"
while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
  token_file:*)
    printf 'token_file: /etc/transitforge/agent.token\n'
    ;;
  dry_run_only:*)
    printf 'dry_run_only: true\n'
    ;;
  *)
    printf '%s\n' "$line"
    ;;
  esac
done <"$LIVE_YAML" >"$tmp"
mv -f -- "$tmp" "$LIVE_YAML"

if grep -qE 'token_file:[[:space:]]*.*bootstrap\.token' "$LIVE_YAML"; then
  die "live agent yaml still points at bootstrap.token"
fi
if grep -qE 'dry_run_only:[[:space:]]*false' "$LIVE_YAML"; then
  die "refusing to start: dry_run_only is false (flip to false only by manual edit after reviewing the plan)"
fi

log "starting controller only (live agent stays down until the token is in place)"
if [ -n "${TRANSITFORGE_VERSION:-}" ]; then
  compose pull
else
  compose build controller bootstrap
fi
compose up -d controller

log "waiting for controller https://127.0.0.1:8443/readyz"
ok=0
i=1
while [ "$i" -le 60 ]; do
  if curl -fsS -k https://127.0.0.1:8443/readyz >/dev/null 2>&1; then
    ok=1
    break
  fi
  sleep 2
  i=$((i + 1))
done
if [ "$ok" -ne 1 ]; then
  die "controller did not become ready"
fi

# Token bytes never go through a shell variable, echo, or `set -x` expansion.
xtrace_off() {
  case "$-" in
  *x*)
    set +x
    TRANSITFORGE_XTRACE_RESTORE=1
    ;;
  *)
    TRANSITFORGE_XTRACE_RESTORE=0
    ;;
  esac
}
xtrace_restore() {
  if [ "${TRANSITFORGE_XTRACE_RESTORE:-0}" -eq 1 ]; then
    set -x
  fi
}

ensure_token_file() {
  log "creating ${TOKEN_HOST} as 0600 root:root before any secret bytes are written"
  umask 077
  install -d -m 0700 /etc/transitforge
  install -m 0600 -o root -g root /dev/null "$TOKEN_HOST"
}

if [ -n "${TRANSITFORGE_AGENT_TOKEN:-}" ]; then
  log "writing TRANSITFORGE_AGENT_TOKEN into ${TOKEN_HOST} (value not logged)"
  xtrace_off
  ensure_token_file
  printf '%s\n' "${TRANSITFORGE_AGENT_TOKEN}" >"$TOKEN_HOST"
  xtrace_restore
elif [ -s "$TOKEN_HOST" ]; then
  log "reusing existing ${TOKEN_HOST}"
  chmod 0600 -- "$TOKEN_HOST"
  chown root:root -- "$TOKEN_HOST"
else
  log "minting agent-role token name=${TOKEN_NAME} into ${TOKEN_HOST}"
  xtrace_off
  ensure_token_file
  set +e
  compose --profile cli run --rm -T \
    -v "${TOKEN_HOST}:/var/lib/transitforge/agent.token" \
    transitforge token add --name "$TOKEN_NAME" --role agent --out-file /var/lib/transitforge/agent.token \
    >/dev/null
  mint_rc=$?
  set -e
  xtrace_restore
  if [ "$mint_rc" -ne 0 ]; then
    die "token mint failed (rc=${mint_rc}). If the name already exists, export TRANSITFORGE_AGENT_TOKEN to ${TOKEN_HOST}."
  fi
  log "minted agent token written to ${TOKEN_HOST}"
fi

if [ ! -s "$TOKEN_HOST" ]; then
  die "agent token file is empty: ${TOKEN_HOST}"
fi

log "comparing ${TOKEN_HOST} to bootstrap token (paths only; values not printed)"
xtrace_off
umask 077
boot_cmp=/run/transitforge/bootstrap.token.cmp
install -d -m 0700 /run/transitforge
install -m 0600 -o root -g root /dev/null "$boot_cmp"
compose exec -T controller cat /data/bootstrap.token | tr -d '\r' >"$boot_cmp"
same=0
if cmp -s -- "$TOKEN_HOST" "$boot_cmp"; then
  same=1
fi
rm -f -- "$boot_cmp"
xtrace_restore
if [ "$same" -eq 1 ]; then
  die "agent token matches bootstrap operator token; refusing to start live overlay"
fi

log "verifying token role via /api/v1/whoami"
who=$(compose --profile cli run --rm -T \
  -v "${TOKEN_HOST}:/etc/transitforge/agent.token:ro" \
  --entrypoint transitforge \
  transitforge --controller https://controller:8443 --token-file /etc/transitforge/agent.token --insecure whoami)
role=$(printf '%s\n' "$who" | json_field role)
if [ "$role" != "agent" ]; then
  die "token role is '${role}', want agent. whoami: ${who}"
fi

log "starting live overlay (dry_run_only=true) + prometheus profile obs"
if [ -n "${TRANSITFORGE_VERSION:-}" ]; then
  compose_live --profile obs pull
fi
compose_live --profile obs up -d "${BUILD_OPTS[@]}"

cat <<EOF
transitforge-smoke: live overlay is up with dry_run_only: true and an agent-role token.

Watch reconcile / dry-run plans:
  docker compose -f docker-compose.yml -f docker-compose.live.yml logs -f agent

Prometheus scrape (must be agent-host:9101, not agent:9101):
  http://127.0.0.1:9090/targets
  host metrics: http://127.0.0.1:9101/metrics

dry_run_only: false is not automated. After you have reviewed the plan, edit
configs/docker-agent.live.yaml by hand and recreate the agent container.

Released images (optional): TRANSITFORGE_VERSION=sha-<shortsha>
EOF
