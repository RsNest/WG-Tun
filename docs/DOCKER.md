# Docker

Canonical build, test, and release path is **Docker only**. A machine with Docker Engine, Buildx, and the Compose plugin does not need Go installed.

Host `go test` / `go build` remains optional for local iteration.

## Images

| Component | GHCR name | Runtime user |
| --- | --- | --- |
| controller | `ghcr.io/rsnest/wg-tun-controller` | non-root `proxyctl` (uid 65532); entrypoint chowns `/data` |
| agent | `ghcr.io/rsnest/wg-tun-agent` | root (live overlay needs NET_ADMIN / host net) |
| proxctl | `ghcr.io/rsnest/wg-tun-proxctl` | root; also used as the Compose bootstrap one-shot |

Platform published in this phase: **linux/amd64**. The Dockerfile is structured so linux/arm64 can be added later without a second build system.

Do not deploy from a mutable `latest` tag. The publish workflow sets `latest=false`; `latest` is not published.

## Tags

| Tag | When | Use |
| --- | --- | --- |
| `sha-<short>` | every push to `main` and every `v*` tag | **immutable deploy / rollback** |
| `main` | push to `main` | moving CI convenience; not for production rollback |
| `v0.1.0` (git tag) | Git tag `v0.1.0` | release pin |
| `v0.1` / `v0` | same tag, if semver | convenience; still prefer the exact tag or SHA |

## Developer validation (fresh checkout, Docker only)

```bash
docker build --target test .
docker build --target controller -t ghcr.io/rsnest/wg-tun-controller:local .
docker build --target agent -t ghcr.io/rsnest/wg-tun-agent:local .
docker build --target proxctl -t ghcr.io/rsnest/wg-tun-proxctl:local .
```

Optional wrapper: `make test` / `make images` / `make smoke`.

Source-build lab (control plane, agent `dry_run_only: true`):

```bash
docker compose up -d --build
docker compose --profile cli run --rm proxctl version
docker compose --profile cli run --rm proxctl apply --node ru-edge-1 --dry-run
```

Optional Web UI (HTTP, separate port; not required for controller startup):

```bash
docker compose -f docker-compose.yml -f docker-compose.ui.yml up -d --build
```

Then open `http://127.0.0.1:8444`. Bind the host publish to loopback in production (`deploy/compose/controller.ui.yml`).

That starts:

1. **controller** — TLS API on `https://localhost:8443`, SQLite + bootstrap token in volume `proxyctl-data`. Healthcheck is `proxyctl-controller healthcheck --url https://127.0.0.1:8443/readyz -k` (no curl in the image). `/readyz` waits for SQLite + init.
2. **bootstrap** — one-shot `proxctl` image that waits for `/readyz`, registers lab inventory, prints a dry-run plan, exits.
3. **agent** — polls desired state every 10s. Default `dry_run_only: true`. Metrics `http://localhost:9101`.

## CI/CD

```
GitHub (push/PR/tag)
  -> GitHub Actions
  -> Docker Buildx (linux/amd64)
  -> test target (gofmt check, go vet, go test)
  -> runtime images
  -> GHCR (main + v* only)
  -> operator pulls an immutable tag
```

- PRs and pushes run `.github/workflows/validate.yml`: Docker test + build all three images. **No push.** No GHCR credentials required.
- Pushes to `main` and tags `v*` run `.github/workflows/publish.yml`: test, build, smoke, then push all three images with provenance/SBOM. Uses `GITHUB_TOKEN` (`packages: write`). A failed test or build prevents that job from publishing; pushes are sequential after smoke.

GHCR package visibility is **not** forced public by the workflow. After the first publish, set visibility under the GitHub package settings if other machines need to pull without authentication.

## Deployment (pull images)

Production/lab hosts consume GHCR images. They do not compile source.

Controller:

```bash
export PROXYCTL_VERSION=sha-abcdef1   # or v0.1.0
docker compose -f deploy/compose/controller.yml pull
docker compose -f deploy/compose/controller.yml up -d
```

Inspect the running digest before changing versions:

```bash
docker inspect --format '{{.Name}} {{.Image}} {{.Config.Image}}' $(docker compose -f deploy/compose/controller.yml ps -q)
```

Optional UI:

```bash
docker compose -f deploy/compose/controller.yml -f deploy/compose/controller.ui.yml up -d
```

Lab stack from GHCR (same version on controller, agent, proxctl):

```bash
export PROXYCTL_VERSION=sha-abcdef1
docker compose -f docker-compose.yml -f docker-compose.release.yml pull
docker compose -f docker-compose.yml -f docker-compose.release.yml up -d
```

Do not hardcode `main` as `PROXYCTL_VERSION`.

## Update

Image publication is automatic. Production replacement is **manual**.

1. Choose an immutable tag (`sha-…` or `v…`).
2. Pull.
3. Inspect image id/digest.
4. Recreate.
5. Check `/healthz`, `/readyz`, and a plan.

```bash
export PROXYCTL_VERSION=sha-89abcde
docker compose -f deploy/compose/controller.yml pull
docker compose -f deploy/compose/controller.yml up -d
```

Do not run Watchtower or any automatic live-agent updater.

## Rollback

Rollback is selecting the previous immutable tag:

```bash
# previous: PROXYCTL_VERSION=sha-1234567
export PROXYCTL_VERSION=sha-1234567
docker compose -f deploy/compose/controller.yml pull
docker compose -f deploy/compose/controller.yml up -d
```

Do not roll back via the moving `main` tag.

## What Docker owns vs what stays on the host

| Layer | Compose default | Live overlay (`docker-compose.live.yml`) |
| --- | --- | --- |
| REST API, SQLite, HMAC, CLI | yes | yes |
| Agent reconcile / dry-run plans | yes | yes |
| iptables / WireGuard on the **public NIC** | no (dry-run) | Linux only: `network_mode: host` + `NET_ADMIN,NET_RAW` + `/dev/net/tun` |
| HAProxy process + `systemctl reload` | no | **host systemd path unit**, not the container |
| SSH TUN `systemctl start` | no | host units; the image does not ship `systemctl` |

The live agent does **not** mount the host systemd control socket. It does not take ownership of SSH TUN units. It does not replace the host HAProxy path unit. Capabilities stay `NET_ADMIN` + `NET_RAW` (no `privileged: true`).

`haproxy_reload: external`: the agent writes `/etc/haproxy` (bind-mounted) and runs `haproxy -c`. A host path unit reloads HAProxy when the file changes. Production edges should prefer a systemd agent; this overlay is for Linux lab hosts.

Agent image packages (from actual `CommandRunner` usage): `ip` (iproute2), `wg` (wireguard-tools), `iptables` / `iptables-save` / `iptables-restore`, `haproxy` (`-c` only), `ping` (overlay health). Controller and proxctl images do not include those tools.

## TLS names

Self-signed certs include SAN `localhost`, `127.0.0.1`, `::1`, plus `tls.dns_names` from YAML (`controller` in Compose). Lab clients still use skip-verify because the CA is untrusted.

`EnsureSelfSigned` does **not** rewrite an existing cert/key pair. After adding `dns_names`, recreate the volume (or delete `/data/certs` in it).

## Config

- `configs/docker-controller.yaml` — listen `0.0.0.0:8443`, data `/data`, SAN `controller`
- `configs/docker-agent.yaml` — `dry_run_only: true`, `https://controller:8443`
- `configs/docker-agent.live.yaml` — `dry_run_only: true`, `https://127.0.0.1:8443`, `haproxy_reload: external`, `token_file: /etc/proxyctl/agent.token`

Token path for the default (bridge) agent is `/data/bootstrap.token`. The live overlay uses `/etc/proxyctl/agent.token` (agent-role), never the bootstrap operator token.

`proxctl` reads `PROXYCTL_CONTROLLER`, `PROXYCTL_TOKEN`, `PROXYCTL_TOKEN_FILE`, `PROXYCTL_INSECURE`. Flags override env.

Binaries report link-time metadata:

```text
proxyctl-controller --version
proxyctl-agent --version
proxctl version
```

Fallback without `-ldflags`: `dev` / `unknown`.

## Live Linux

Requires Docker Engine on Linux (not Docker Desktop for Windows). Host network cannot publish ports or set `hostname:` (that would rename the host), so the overlay resets both.

```bash
sudo install -d -m 0755 /usr/local/lib/proxyctl /var/lib/proxyctl
sudo install -m 0755 scripts/haproxy-reload-on-change.sh /usr/local/lib/proxyctl/haproxy-reload-on-change.sh
sudo install -m 0644 deploy/systemd/proxyctl-haproxy-reload.path /etc/systemd/system/proxyctl-haproxy-reload.path
sudo install -m 0644 deploy/systemd/proxyctl-haproxy-reload.service /etc/systemd/system/proxyctl-haproxy-reload.service
sudo systemctl daemon-reload
sudo systemctl enable --now proxyctl-haproxy-reload.path

sudo bash scripts/run-smoke-tests.sh
sudo bash scripts/enable-live-overlay.sh
# or, after images exist in GHCR:
# sudo env PROXYCTL_VERSION=sha-abcdef1 bash scripts/enable-live-overlay.sh
```

`enable-live-overlay.sh` still requires the smoke stamp, an agent-role token (never bootstrap), and `dry_run_only: true`. It never flips `dry_run_only` to false.

The live agent talks to the controller on the host loopback (`https://127.0.0.1:8443`). Load `wireguard` on the host (`modprobe wireguard`).

## Prometheus (live)

Default `deploy/prometheus/prometheus.docker.yml` scrapes `agent:9101` on the Compose bridge. With `network_mode: host` use `prometheus.live.yml` and `extra_hosts: ["agent-host:host-gateway"]`. Agent `metrics_listen` is `0.0.0.0:9101`.

## Dockerfile targets

- `test` — `gofmt -l` (fail if dirty), `go vet ./...`, `go test ./...`
- `controller` — `proxyctl-controller`; HEALTHCHECK via `healthcheck` subcommand
- `agent` — `proxyctl-agent` + runtime tools listed above
- `proxctl` — CLI + `docker-bootstrap.sh`
