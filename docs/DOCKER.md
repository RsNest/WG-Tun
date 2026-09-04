# Docker

proxyctl runs as three images from one Dockerfile (`controller`, `agent`, `proxctl`).

```
docker compose up -d --build
```

That starts:

1. **controller** — TLS API on `https://localhost:8443`, SQLite + bootstrap token in volume `proxyctl-data`. Compose `healthcheck` calls `proxyctl-controller healthcheck --url https://127.0.0.1:8443/readyz -k` (binary, no curl/wget). `depends_on: condition: service_healthy` needs this; `/readyz` waits for SQLite + init, not just process liveness.
2. **bootstrap** — one-shot `proxctl` that waits for `/readyz`, registers `ru-edge-1`, backend, UDP+TCP mappings, prints a dry-run plan, exits. It passes `--controller` / `--token-file` / `--insecure` and also sets `PROXYCTL_CONTROLLER`, `PROXYCTL_TOKEN_FILE`, `PROXYCTL_INSECURE`.
3. **agent** — polls desired state every 10s. Default `dry_run_only: true` (safe on Docker Desktop / Windows). `/healthz` `/readyz` `/metrics` on `http://localhost:9101`. ENTRYPOINT includes `--insecure`; YAML has `tls.insecure_skip_verify: true`.

```
docker compose --profile cli run --rm proxctl node list
docker compose --profile cli run --rm proxctl apply --node ru-edge-1 --dry-run
docker compose --profile obs up -d
```

## What Docker owns vs what stays on the host

| Layer | In Compose (default) | Live overlay (`docker-compose.live.yml`) |
| --- | --- | --- |
| REST API, SQLite, HMAC, CLI | yes | yes |
| Agent reconcile / dry-run plans | yes | yes |
| iptables / WireGuard on the **public NIC** | no (dry-run) | Linux only: `network_mode: host` + `cap_add: NET_ADMIN,NET_RAW` + `/dev/net/tun` |
| HAProxy process + `systemctl reload` | no | **host systemd**, not the container |

`privileged: true` is not used. Capabilities + `/dev/net/tun` are enough for WG/iptables in the host netns; widen only if a kernel op still fails.

`systemctl reload haproxy` from a container does not reload the host unit, even with `/etc/haproxy` bind-mounted. Live Docker agent sets `haproxy_reload: external`: write the managed section, run `haproxy -c`, skip systemd. A host path unit reloads HAProxy when the file changes. Production edges should run the agent as a systemd unit, not in Docker.

SSH TUN `systemctl start` has the same limit: the live container will not start host units.

## TLS names

Self-signed certs include SAN `localhost`, `127.0.0.1`, `::1`, plus `tls.dns_names` from YAML (`controller` in Compose). Lab clients still use skip-verify because the CA is untrusted.

`EnsureSelfSigned` does **not** rewrite an existing cert/key pair. After adding `dns_names`, recreate the volume (or delete `/data/certs` in it):

```
docker compose down
docker volume rm proxyctl_proxyctl-data
```

## Images

`Dockerfile` targets:

- `controller` — `proxyctl-controller`, HEALTHCHECK via `healthcheck` subcommand
- `agent` — `proxyctl-agent` + `ip` / `iptables` / `wg` / `ping` / `haproxy` (`-c` only)
- `proxctl` — CLI + `docker-bootstrap.sh`

## Config

- `configs/docker-controller.yaml` — listen `0.0.0.0:8443`, data `/data`, SAN `controller`
- `configs/docker-agent.yaml` — `dry_run_only: true`, `https://controller:8443`
- `configs/docker-agent.live.yaml` — `dry_run_only: true`, `https://127.0.0.1:8443`, `haproxy_reload: external`, `token_file: /etc/proxyctl/agent.token`

Token path for the default (bridge) agent is `/data/bootstrap.token`. The live overlay uses `/etc/proxyctl/agent.token` (agent-role), never the bootstrap operator token.

`proxctl` reads `PROXYCTL_CONTROLLER`, `PROXYCTL_TOKEN`, `PROXYCTL_TOKEN_FILE`, `PROXYCTL_INSECURE` (`1`/`true`/`yes`/`on`). Flags override env.

## Live Linux

Requires Docker Engine on Linux (not Docker Desktop for Windows). Host network cannot publish ports or set `hostname:` (that would rename the host), so the overlay resets both.

```
# once on the host, before first live SNI apply
sudo install -d -m 0755 /usr/local/lib/proxyctl /var/lib/proxyctl
sudo install -m 0755 scripts/haproxy-reload-on-change.sh /usr/local/lib/proxyctl/haproxy-reload-on-change.sh
sudo install -m 0644 deploy/systemd/proxyctl-haproxy-reload.path /etc/systemd/system/proxyctl-haproxy-reload.path
sudo install -m 0644 deploy/systemd/proxyctl-haproxy-reload.service /etc/systemd/system/proxyctl-haproxy-reload.service
sudo systemctl daemon-reload
sudo systemctl enable --now proxyctl-haproxy-reload.path

docker compose -f docker-compose.yml -f docker-compose.live.yml up -d --build
```

The live agent talks to the controller on the host loopback (`https://127.0.0.1:8443`). Compose DNS name `controller` is not in the host netns. Load the WireGuard kernel module on the host before apply (`modprobe wireguard`); the container will not load it.

`depends_on` for the agent (`controller` healthy, `bootstrap` completed) comes from `docker-compose.yml` and is not reset by the overlay.

## Prometheus (live)

Default `deploy/prometheus/prometheus.docker.yml` scrapes `agent:9101`. That name exists only while the agent is on the Compose bridge. With `network_mode: host` it is gone, and Prometheus on the bridge would fail with `no such host` / `context deadline exceeded`.

The live overlay mounts sibling `prometheus.live.yml` and adds `extra_hosts: ["agent-host:host-gateway"]` (Docker ≥ 20.10, Linux Engine). Scrape target is `agent-host:9101`. That hits the host bridge IP, so the agent must bind `0.0.0.0:9101` (not `127.0.0.1`). Controller scrape stays `controller:8443` on the Compose network.

```
docker compose -f docker-compose.yml -f docker-compose.live.yml --profile obs up -d --build
```

## Lab sequence (before a real edge)

On a disposable Debian/Ubuntu VM:

```
sudo bash scripts/run-smoke-tests.sh
sudo bash scripts/enable-live-overlay.sh
```

`enable-live-overlay.sh` starts the live overlay with `dry_run_only: true` and an agent-role token. Review `docker compose -f docker-compose.yml -f docker-compose.live.yml logs -f agent` and Prometheus `agent-host:9101`. Flip `dry_run_only` to `false` only by hand after that review. See `docs/OPERATIONS.md`.
