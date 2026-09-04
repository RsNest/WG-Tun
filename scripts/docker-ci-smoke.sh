#!/usr/bin/env bash
# Assert Docker-built proxyctl images: version output, controller health, no checkout secrets.
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

name="proxyctl-ci-ctrl-$$"
cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "controller packaged config, plain-http healthz/readyz"
docker run -d --name "$name" --no-healthcheck \
  "$CONTROLLER_IMAGE" \
  --config /etc/proxyctl/controller.yaml --plain-http --listen 0.0.0.0:8080
ok=0
i=1
while [ "$i" -le 30 ]; do
  if docker exec "$name" /usr/local/bin/proxyctl-controller healthcheck --url http://127.0.0.1:8080/healthz \
    && docker exec "$name" /usr/local/bin/proxyctl-controller healthcheck --url http://127.0.0.1:8080/readyz; then
    ok=1
    break
  fi
  sleep 1
  i=$((i + 1))
done
if [ "$ok" -ne 1 ]; then
  docker logs "$name" >&2 || true
  echo "controller did not become ready" >&2
  exit 1
fi

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

echo "agent dry-run start (no live mutation; dummy token; no controller)"
aname="proxyctl-ci-agent-$$"
atok="$(mktemp)"
cleanup_agent() {
  docker rm -f "$aname" >/dev/null 2>&1 || true
  rm -f "$atok"
}
trap 'cleanup; cleanup_agent' EXIT
printf 'ci-smoke-token\n' >"$atok"
docker run -d --name "$aname" --network none --no-healthcheck \
  -v "$atok:/data/bootstrap.token:ro" \
  "$AGENT_IMAGE"
aok=0
i=1
while [ "$i" -le 30 ]; do
  if docker exec "$aname" /usr/local/bin/proxyctl-agent healthcheck --url http://127.0.0.1:9101/healthz; then
    aok=1
    break
  fi
  sleep 1
  i=$((i + 1))
done
if [ "$aok" -ne 1 ]; then
  docker logs "$aname" >&2 || true
  echo "agent did not become ready" >&2
  exit 1
fi
echo "docker-ci-smoke: ok"
