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

echo "controller plain-http healthz/readyz"
docker run -d --name "$name" --no-healthcheck \
  "$CONTROLLER_IMAGE" \
  --plain-http --listen 0.0.0.0:8080 --data-dir /tmp/proxyctl-ci
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

echo "agent starts (--version already proved binary); dry-run config is image default"
docker inspect --format '{{.Config.User}}' "$AGENT_IMAGE" >/dev/null
echo "docker-ci-smoke: ok"
