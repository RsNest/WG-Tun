#!/usr/bin/env bash
# Wait for controller, then register the lab edge node (idempotent).
# transitforge reads TRANSITFORGE_CONTROLLER / TRANSITFORGE_TOKEN_FILE / TRANSITFORGE_INSECURE;
# flags are passed as well so bootstrap does not depend on env-only parsing.
set -euo pipefail

CONTROLLER="${TRANSITFORGE_CONTROLLER:-https://controller:8443}"
TOKEN_FILE="${TRANSITFORGE_TOKEN_FILE:-/data/bootstrap.token}"
NODE_NAME="${TRANSITFORGE_NODE_NAME:-ru-edge-1}"
PUBLIC_IP="${TRANSITFORGE_NODE_PUBLIC_IP:-203.0.113.10}"
BACKEND_NAME="${TRANSITFORGE_BACKEND_NAME:-backend-a}"
BACKEND_ADDR="${TRANSITFORGE_BACKEND_ADDR:-10.200.1.2}"

export TRANSITFORGE_CONTROLLER="$CONTROLLER"
export TRANSITFORGE_TOKEN_FILE="$TOKEN_FILE"
export TRANSITFORGE_INSECURE="${TRANSITFORGE_INSECURE:-true}"

echo "waiting for controller $CONTROLLER and token $TOKEN_FILE"
ok=0
for _ in $(seq 1 60); do
  if [ -s "$TOKEN_FILE" ] && wget -q --no-check-certificate -O- "$CONTROLLER/readyz" >/dev/null 2>&1; then
    ok=1
    break
  fi
  sleep 1
done
if [ "$ok" -ne 1 ]; then
  echo "controller did not become ready" >&2
  exit 1
fi

pc() {
  transitforge --controller "$CONTROLLER" --token-file "$TOKEN_FILE" --insecure "$@"
}

if pc node list | grep -q "\"name\": \"$NODE_NAME\""; then
  echo "node $NODE_NAME already registered"
else
  pc node add --name "$NODE_NAME" --public-ip "$PUBLIC_IP"
  echo "registered node $NODE_NAME"
fi

if pc backend list | grep -q "\"name\": \"$BACKEND_NAME\""; then
  echo "backend $BACKEND_NAME already registered"
else
  pc backend add --name "$BACKEND_NAME" --node "$NODE_NAME" --address "$BACKEND_ADDR"
  echo "registered backend $BACKEND_NAME"
fi

if ! pc mapping list | grep -q '"public_port": 51821'; then
  pc mapping add --node "$NODE_NAME" --backend "$BACKEND_NAME" --protocol UDP --public-port 51821 --backend-port 51820
fi
if ! pc mapping list | grep -q '"public_port": 443'; then
  pc mapping add --node "$NODE_NAME" --backend "$BACKEND_NAME" --protocol TCP --public-port 443 --backend-port 443
fi

echo "bootstrap complete"
pc apply --node "$NODE_NAME" --dry-run || true
