# Failover

Per backend that has both a `WIREGUARD` tunnel and an `SSH_TUN` tunnel.

## States

`WG_PRIMARY` → (automatic, after `failure_threshold` consecutive failed cycles, default 3) → `FAILBACK_IN_PROGRESS` → `SSH_PRIMARY`

`SSH_PRIMARY` → `WG_PRIMARY` **only** via operator Failback: `POST /api/v1/nodes/{id}/failback`. Never automatic, even if `automatic_failforward` is true (the flag is ignored as a safety default). The stored intent may still use the internal action name `fail_forward`.

If neither transport is healthy: `DEGRADED` and log `CRITICAL_NO_HEALTHY_TRANSPORT`. Traffic is not blindly rerouted.

## Primary unhealthy

Interface missing, **or** (enough backend probes failing **and** (handshake stale >180s **or** overlay ping failed)).

## SSH healthy (required before cutover)

systemd unit active **and** interface present **and** overlay ping **and** TCP probes succeeding.

## Persistence

- `/run/transitforge/transport-state.json`
- flock on `/run/transitforge/transport.lock` (timeout 5s, meta TTL 30s, never steal)

## Cutover order

lock → snapshot firewall/HAProxy/state → start SSH unit if needed → verify fallback → rewrite DNAT destinations to the SSH overlay IP → verify → commit state.

On failure: restore firewall, restore HAProxy, `systemctl reload haproxy`, restore transport state.

## Operator Failback

```bash
transitforge failback --node ru-edge-1 --backend backend-a
```

Failback is the explicit human-triggered transition `SSH_PRIMARY` → `WG_PRIMARY`. The controller stores the intent (HTTP 202); the agent consumes it on the next cycle if WireGuard is healthy.
