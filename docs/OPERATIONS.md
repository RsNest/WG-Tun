# Operations

## Controller

```bash
proxyctl-controller --config /etc/proxyctl/controller.yaml
# lab:
proxyctl-controller --plain-http --listen 127.0.0.1:8080 --data-dir ./data
```

Bootstrap token path: `auth.bootstrap_token_file` (default `<data_dir>/bootstrap.token`). If the DB already has the bootstrap hash and the file is missing, the process refuses to invent a new token.

`/readyz` is 503 until SQLite is reachable and the server is marked initialized. `/healthz` only means the process is alive.

## Web UI

Off by default. Enable with `--ui-listen` (HTTP, separate from `--listen`):

```bash
proxyctl-controller --config /etc/proxyctl/controller.yaml --ui-listen 127.0.0.1:8444
```

Then browse `http://127.0.0.1:8444`. Sign in with an operator or readonly API token (file `auth.bootstrap_token_file` or a minted token). The UI stores the token only in memory, tied to a signed HTTP-only cookie (`proxyctl_ui`, 12h). Restarting the controller invalidates all UI sessions. Logout clears the cookie.

**Do not bind this to `0.0.0.0` without the same controls as the API.** It is a localhost operator console. There is no mTLS on the UI listener.

Pages: dashboard (node cards, ~8s HTMX poll; a failed poll keeps the last cards and shows a refresh error), nodes (diagnostic detail), backends, tunnels (WireGuard create; key **path** only), mappings (enable/disable via `PATCH /api/v1/mappings/{id}`), SNI routes, events (`GET /events` calls `GET /api/v1/events?node=&backend=&since=&until=&action=`).

**Refresh plan vs audited dry-run.** On the node page, **Refresh plan** calls `GET /api/v1/nodes/{id}/plan`: read-only Diff, no apply audit. **Run audited dry-run** (operators) calls `POST /api/v1/nodes/{id}/apply` with `{"dry_run": true}` and records `apply-dry-run`. Do not treat them as the same control or contract.

**Apply while LiveApply is disabled.** This controller binary has `LiveApply` off. The UI Apply button is disabled and the node page shows `Live apply is not enabled on this controller.` Refresh plan and audited dry-run stay available to the operator role. Live mutation remains agent-driven (`dry_run_only` on the agent).

**Failback** is the explicit human-triggered transition `SSH_PRIMARY` → `WG_PRIMARY` (`POST /api/v1/nodes/{id}/failback`, HTTP 202). The control is shown only when the node is on SSH fallback. Confirm dialog: `Switch this node from SSH fallback back to WireGuard?`

**Mapping `enabled`.** `true` means the mapping is in desired-state. `false` keeps it in inventory and omits it from desired-state; if it still exists on the node, the next plan contains the corresponding DELETE. Unchanged disabled state after convergence is `NO CHANGES`.

**Actual-state.** Agents `POST /api/v1/nodes/{id}/actual-state`. Operators and readonly users `GET` the same path for the last stored actual-state plus agent status. That GET does not expose secrets.

## Agent

```bash
proxyctl-agent --config /etc/proxyctl/agent.yaml
```

Keep `dry_run_only: true` until you have reviewed dry-run plans on that node. Live apply mutates iptables, WireGuard, HAProxy, and may `systemctl start` an existing SSH TUN unit.

State directory default: `/run/proxyctl` (`transport-state.json`, backups).

## CLI

```bash
proxctl --controller https://127.0.0.1:8443 --token-file ./data/bootstrap.token --insecure \
  apply --node ru-edge-1 --dry-run
```

Env (flags win): `PROXYCTL_CONTROLLER`, `PROXYCTL_TOKEN`, `PROXYCTL_TOKEN_FILE`, `PROXYCTL_INSECURE`.

`apply` without `--dry-run` still returns a controller-side plan from last actual-state and records an `apply-dry-run` audit event while LiveApply is off; **live mutation is agent-driven**. For a read-only preview with no apply audit, use `GET /api/v1/nodes/{id}/plan`.

## Backups

- iptables: `iptables-save` into the agent backup dir before writes
- HAProxy: timestamped copy of the whole file before writes; `haproxy -c`; then `systemctl reload` unless `haproxy_reload: external` (see [Host HAProxy reload](#host-haproxy-reload) for the Docker live path unit). Never `restart`.

## Rollback

Apply is transactional per reconcile: on verify failure the engine rolls back firewall (inverse rules / snapshot), HAProxy file+reload, and WireGuard interfaces created in that transaction. Failback cutover additionally restores `transport-state.json`. The host is never rebooted.

## Host HAProxy reload

Live Docker agents set `haproxy_reload: external`: they write `/etc/haproxy/haproxy.cfg` (atomic rename) and run `haproxy -c` inside the container, but they cannot reload the host unit. The host path unit does that.

Enable once (does not depend on the agent container or network):

```bash
sudo install -d -m 0755 /usr/local/lib/proxyctl /var/lib/proxyctl
sudo install -m 0755 scripts/haproxy-reload-on-change.sh /usr/local/lib/proxyctl/haproxy-reload-on-change.sh
sudo install -m 0644 deploy/systemd/proxyctl-haproxy-reload.path /etc/systemd/system/proxyctl-haproxy-reload.path
sudo install -m 0644 deploy/systemd/proxyctl-haproxy-reload.service /etc/systemd/system/proxyctl-haproxy-reload.service
sudo systemctl daemon-reload
sudo systemctl enable --now proxyctl-haproxy-reload.path
```

`proxyctl-haproxy-reload.path` watches `PathModified` and `PathChanged` on `/etc/haproxy/haproxy.cfg` (`PathChanged` is required because the agent replaces the file by rename). It starts `proxyctl-haproxy-reload.service` (oneshot). The script:

1. Takes `/run/proxyctl/haproxy-reload.lock` (`flock`) so `haproxy -c` and `systemctl reload` never overlap. A trigger that arrives during a run waits, then does a follow-up pass.
2. Debounces ~2s of stable content hash (resets the wait on each new write, caps at 12s) so a batched reconcile collapses to one reload.
3. No-ops if the SHA-256 matches `/var/lib/proxyctl/haproxy-reload.state` (touch / identical rewrite).
4. Validates with `haproxy -c -f /etc/haproxy/haproxy.cfg`. On failure it restores `/var/lib/proxyctl/haproxy.last-good.cfg` over the live path and does **not** reload.
5. Snapshots last-good into `/var/lib/proxyctl/haproxy-backups/`, then `systemctl reload haproxy`. Reload failure also restores last-good. Success updates last-good and the state hash.

Journal lines are prefixed `proxyctl-haproxy-reload:` with `event=` (`change_detected`, `debounce_window_started`, `debounce_window_reset`, `validation_started`, `validation_passed`, `validation_failed`, `reload_started`, `reload_succeeded`, `reload_failed`, `restore_on_failure_triggered`, `reload_skipped`).

```bash
systemctl status proxyctl-haproxy-reload.path
systemctl status proxyctl-haproxy-reload.service
journalctl -u proxyctl-haproxy-reload.service -e
journalctl -t proxyctl-haproxy-reload -e
journalctl -u proxyctl-haproxy-reload.service | grep event=
```

Force a pass (same script, useful after a manual edit):

```bash
sudo systemctl start proxyctl-haproxy-reload.service
```

`haproxy-reload.state` and `haproxy.last-good.cfg` live under `/var/lib/proxyctl` and survive reboot (idempotency). `/run/proxyctl/haproxy-reload.debounce` is tmpfs and resets on reboot, which is intended. Snapshot into `haproxy-backups/` does not move last-good; last-good and the state hash are committed only after `reload_succeeded`. If systemd hits `TimeoutStartSec=120` (SIGKILL) between snapshot and reload, the next trigger retries the new file against the still-old last-good.

## Disposable VM smoke tests

On a throwaway Debian/Ubuntu box (SSH, nothing else required). From the repo, as root:

```bash
bash scripts/run-smoke-tests.sh
```

That installs Docker Engine, HAProxy, WireGuard tools, the path unit (not the live agent), then runs `scripts/smoke-test-debounce.sh` and `scripts/smoke-test-restore.sh`. Ctrl-C restores a valid `/etc/haproxy/haproxy.cfg`. A stamp is written to `/var/lib/proxyctl/smoke-tests.passed` only if both tests pass.

## Enable live overlay (dry-run)

The only supported way to start the host-network agent after smoke tests:

```bash
bash scripts/enable-live-overlay.sh
# GHCR instead of --build (smoke gates unchanged):
# env PROXYCTL_VERSION=sha-abcdef1 bash scripts/enable-live-overlay.sh
# or, if you already minted an agent token (do not paste the value into chat/docs):
# PROXYCTL_AGENT_TOKEN='...' bash scripts/enable-live-overlay.sh
```

The script refuses to start if:

- smoke tests have not passed
- `configs/docker-agent.live.yaml` would still use `bootstrap.token` / the operator bootstrap secret
- `dry_run_only` is not `true`
- `/api/v1/whoami` reports a role other than `agent`

It mints an **agent-role** token via `proxctl token add --role agent --out-file` (operator bootstrap is only used to mint). The secret is written to `/etc/proxyctl/agent.token` which is created as `0600 root:root` *before* any bytes are written; the value is never printed to stdout, journal, or the smoke stamp. `dry_run_only: false` is **not** automated: after you have read `docker compose … logs -f agent`, edit `configs/docker-agent.live.yaml` by hand and recreate the agent container.

## Image deploy, update, rollback

Runtime hosts pull `ghcr.io/rsnest/wg-tun-*`. They do not install Go. Pin `PROXYCTL_VERSION` to `sha-<short>` or a `v*` tag. Do not deploy `main` as the rollback handle.

Controller:

```bash
export PROXYCTL_VERSION=sha-abcdef1
docker compose -f deploy/compose/controller.yml pull
docker compose -f deploy/compose/controller.yml up -d
```

Record the running image before updating:

```bash
docker inspect --format '{{.Image}} {{index .RepoDigests 0}}' "$(docker compose -f deploy/compose/controller.yml ps -q controller)"
```

Update is the same commands with a newer SHA. Rollback is the previous SHA:

```bash
export PROXYCTL_VERSION=sha-1234567
docker compose -f deploy/compose/controller.yml pull
docker compose -f deploy/compose/controller.yml up -d
```

Publication of new images is automatic on `main` / `v*` tags. Replacing a live agent is always a human `compose pull` + `up -d` (or recreate). See `docs/DOCKER.md`.


