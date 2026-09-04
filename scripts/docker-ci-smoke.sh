#!/usr/bin/env bash
# Assert Docker-built proxyctl images: version output, controller health, dry-run agent, no checkout secrets.
# Does not mutate iptables/WireGuard. Requires Docker; does not require host Go.
set -euo pipefail

CONTROLLER_IMAGE="${CONTROLLER_IMAGE:?CONTROLLER_IMAGE is required}"
AGENT_IMAGE="${AGENT_IMAGE:?AGENT_IMAGE is required}"
PROXCTL_IMAGE="${PROXCTL_IMAGE:?PROXCTL_IMAGE is required}"

assert_no_secrets() {
  local image="$1"
  docker run --rm --user 0 --entrypoint sh "$image" -c '
    set -eu
    test ! -d /src
    test ! -d /.git
    test ! -d /go
    test ! -f /data/bootstrap.token
    test ! -f /etc/proxyctl/bootstrap.token
    test ! -f /etc/proxyctl/agent.token
    for p in /data/*.db /data/*.sqlite /data/*.key /data/*.pem; do
      if [ -e "$p" ]; then echo "unexpected $p" >&2; exit 1; fi
    done
  '
}

container_running() {
  [ "$(docker inspect -f '{{.State.Running}}' "$1" 2>/dev/null || true)" = "true" ]
}

wait_exec() {
  local cname="$1"
  shift
  local i=1
  while [ "$i" -le 30 ]; do
    if ! container_running "$cname"; then
      docker logs "$cname" >&2 || true
      echo "$cname is not running" >&2
      return 1
    fi
    if docker exec "$cname" "$@" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    i=$((i + 1))
  done
  docker logs "$cname" >&2 || true
  echo "timed out waiting for: $cname $*" >&2
  return 1
}

echo "proxctl --version"
docker run --rm --entrypoint proxctl "$PROXCTL_IMAGE" version
echo "controller --version"
docker run --rm --entrypoint /usr/local/bin/proxyctl-controller "$CONTROLLER_IMAGE" --version
echo "agent --version"
docker run --rm --entrypoint /usr/local/bin/proxyctl-agent "$AGENT_IMAGE" --version

echo "secret/source tree scan"
assert_no_secrets "$CONTROLLER_IMAGE"
assert_no_secrets "$AGENT_IMAGE"
assert_no_secrets "$PROXCTL_IMAGE"

net="proxyctl-ci-net-$$"
name="proxyctl-ci-ctrl-$$"
aname="proxyctl-ci-agent-$$"
tmpdir="$(mktemp -d)"
atok="$tmpdir/bootstrap.token"
ayaml="$tmpdir/agent.yaml"

cleanup() {
  docker rm -f "$name" "$aname" >/dev/null 2>&1 || true
  docker network rm "$net" >/dev/null 2>&1 || true
  rm -rf "$tmpdir"
}
trap cleanup EXIT

echo "agent runtime tools (from CommandRunner usage; systemd stays on the host)"
docker run --rm --user 0 --entrypoint sh "$AGENT_IMAGE" -c '
  set -eu
  command -v ip
  command -v wg
  command -v iptables
  command -v iptables-save
  command -v iptables-restore
  command -v haproxy
  command -v ping
'

echo "controller packaged config, plain-http healthz/readyz"
docker network create "$net" >/dev/null
docker run -d --name "$name" --no-healthcheck \
  --network "$net" --network-alias controller \
  "$CONTROLLER_IMAGE" \
  --config /etc/proxyctl/controller.yaml --plain-http --listen 0.0.0.0:8080 >/dev/null
wait_exec "$name" /usr/local/bin/proxyctl-controller healthcheck --url http://127.0.0.1:8080/healthz
wait_exec "$name" /usr/local/bin/proxyctl-controller healthcheck --url http://127.0.0.1:8080/readyz
uid="$(docker exec "$name" awk '/^Uid:/{print $2}' /proc/1/status)"
if [ "$uid" != "65532" ]; then
  echo "controller pid 1 uid=$uid, want 65532 (non-root)" >&2
  docker logs "$name" >&2 || true
  exit 1
fi

echo "proxctl whoami + register dry-run node"
docker cp "$name:/data/bootstrap.token" "$atok"
chmod 0600 "$atok"
docker run --rm --network "$net" \
  -v "$atok:/token:ro" \
  -e PROXYCTL_CONTROLLER=http://controller:8080 \
  -e PROXYCTL_TOKEN_FILE=/token \
  -e PROXYCTL_INSECURE=true \
  "$PROXCTL_IMAGE" whoami
docker run --rm --network "$net" \
  -v "$atok:/token:ro" \
  -e PROXYCTL_CONTROLLER=http://controller:8080 \
  -e PROXYCTL_TOKEN_FILE=/token \
  -e PROXYCTL_INSECURE=true \
  "$PROXCTL_IMAGE" node add --name ru-edge-1 >/dev/null

cat >"$ayaml" <<'EOF'
node_name: ru-edge-1
controller_url: http://controller:8080
token_file: /data/bootstrap.token
reconcile_interval: 10s
dry_run_only: true
state_dir: /run/proxyctl
haproxy_config: /etc/haproxy/haproxy.cfg
haproxy_reload: external
tls:
  insecure_skip_verify: true
metrics_listen: "0.0.0.0:9101"
EOF

echo "agent dry-run start (shared network, no NET_ADMIN, dummy inventory only)"
docker run -d --name "$aname" --network "$net" --no-healthcheck \
  -v "$atok:/data/bootstrap.token:ro" \
  -v "$ayaml:/etc/proxyctl/agent.yaml:ro" \
  "$AGENT_IMAGE" >/dev/null
wait_exec "$aname" /usr/local/bin/proxyctl-agent healthcheck --url http://127.0.0.1:9101/healthz
wait_exec "$aname" /usr/local/bin/proxyctl-agent healthcheck --url http://127.0.0.1:9101/readyz
auid="$(docker exec "$aname" awk '/^Uid:/{print $2}' /proc/1/status)"
if [ "$auid" != "0" ]; then
  echo "agent pid 1 uid=$auid, want 0 (root; live overlay needs caps)" >&2
  docker logs "$aname" >&2 || true
  exit 1
fi

echo "docker-ci-smoke: ok"
