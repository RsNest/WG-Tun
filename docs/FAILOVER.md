# Failover

Per backend that has both a `WIREGUARD` tunnel and an `SSH_TUN` tunnel.

## States

`WG_PRIMARY` → (automatic, after `failure_threshold` consecutive failed cycles, default 3) → `FAILBACK_IN_PROGRESS` → `SSH_PRIMARY`

`SSH_PRIMARY` → `WG_PRIMARY` **only** via operator `POST /api/v1/nodes/{id}/failback` (`action=fail_forward`). Never automatic, even if `automatic_failforward` is true (the flag is ignored as a safety default).

If neither transport is healthy: `DEGRADED` and log `CRITICAL_NO_HEALTHY_TRANSPORT`. Traffic is not blindly rerouted.

## Primary unhealthy

Interface missing, **or** (enough backend probes failing **and** (handshake stale >180s **or** overlay ping failed)).

## SSH healthy (required before cutover)

systemd unit active **and** interface present **and** overlay ping **and** TCP probes succeeding.

## Persistence

- `/run/proxyctl/transport-state.json`
- flock on `/run/proxyctl/transport.lock` (timeout 5s, meta TTL 30s, never steal)

## Cutover order

lock → snapshot firewall/HAProxy/state → start SSH unit if needed → verify fallback → rewrite DNAT destinations to the SSH overlay IP → verify → commit state.

On failure: restore firewall, restore HAProxy, `systemctl reload haproxy`, restore transport state.

## Operator fail-forward

```bash
proxctl failback --node ru-edge-1 --backend backend-a
```

The controller stores a `fail_forward` intent; the agent consumes it on the next cycle if WireGuard is healthy.
