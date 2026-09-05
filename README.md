# TransitForge

Multi-node edge/proxy infrastructure manager: desired-state controller, rootful edge agent, and `transitforge` CLI.

This software configures **your own** nodes (firewall/NAT, WireGuard, HAProxy, SSH TUN systemd units). It is not an attack tool.

## Architecture

- **controller** — TLS REST API + SQLite desired-state store (nodes, backends, mappings, tunnels, SNI routes, failover intents).
- **agent** — reconciles host state every 10s: WireGuard, iptables managed chains, HAProxy managed sections, SSH TUN unit inspection, failback state machine.
- **transitforge** — operator CLI (HMAC-signed Bearer requests).

Stages 1–5 of the staged MVP are implemented. See `docs/ARCHITECTURE.md` and `docs/REMAINING_WORK.md`.

## Build

Canonical path (Docker Engine + Buildx, **no host Go**):

```bash
docker build --target test .
docker build --target controller -t ghcr.io/rsnest/transitforge-controller:local .
docker build --target agent -t ghcr.io/rsnest/transitforge-agent:local .
docker build --target cli -t ghcr.io/rsnest/transitforge-cli:local .
```

Optional host Go (not required for CI or release artifacts):

```bash
go fmt ./...
go vet ./...
go test ./...
go build -o bin/transitforge-controller ./cmd/controller
go build -o bin/transitforge-agent ./cmd/agent
go build -o bin/transitforge ./cmd/transitforge
```

## Run locally (lab)

```powershell
# controller (plain HTTP for a local lab only)
./bin/transitforge-controller.exe --plain-http --listen 127.0.0.1:8080 --data-dir ./data

# token is written once to ./data/bootstrap.token
$env:TRANSITFORGE_CONTROLLER = "http://127.0.0.1:8080"
$env:TRANSITFORGE_TOKEN = (Get-Content ./data/bootstrap.token).Trim()

./bin/transitforge.exe --controller http://127.0.0.1:8080 --token-file ./data/bootstrap.token --insecure node add --name ru-edge-1 --public-ip 203.0.113.10
./bin/transitforge.exe --controller http://127.0.0.1:8080 --token-file ./data/bootstrap.token --insecure backend add --name backend-a --node ru-edge-1 --address 10.200.1.2
./bin/transitforge.exe --controller http://127.0.0.1:8080 --token-file ./data/bootstrap.token --insecure mapping add --node ru-edge-1 --backend backend-a --protocol UDP --public-port 51821 --backend-port 51820
./bin/transitforge.exe --controller http://127.0.0.1:8080 --token-file ./data/bootstrap.token --insecure mapping add --node ru-edge-1 --backend backend-a --protocol TCP --public-port 443 --backend-port 443
./bin/transitforge.exe --controller http://127.0.0.1:8080 --token-file ./data/bootstrap.token --insecure apply --node ru-edge-1 --dry-run
```

Default controller listen is `127.0.0.1:8443` with TLS (`tls.auto_self_signed: true` in `configs/example-controller.yaml`). Use `--insecure` with transitforge against the lab cert.

The example agent config has `dry_run_only: true` so it will not mutate iptables/WireGuard/HAProxy until you set that to false on a real edge node.

## Web UI

Optional operator console, compiled into the controller binary (Go `html/template` + HTMX). Disabled unless you set `--ui-listen`.

```powershell
./bin/transitforge-controller.exe --plain-http --listen 127.0.0.1:8080 --ui-listen 127.0.0.1:8444 --data-dir ./data
```

Open `http://127.0.0.1:8444`, paste an **operator** or **readonly** API token (the same bootstrap token `transitforge` uses). The token is exchanged for a 12-hour HTTP-only cookie session and is never written into HTML. Agent-role tokens cannot sign in.

Default bind is your choice at start time; **use loopback**. This is an operator tool, not a public website. If you bind it beyond localhost, put it behind the same network restrictions as the API (VPN, firewall, SSH tunnel). The UI listener is plain HTTP on purpose so it stays off the TLS API port.

Readonly users can browse inventory, plan, status, and events. Apply / Failback / create / toggle / delete are hidden or disabled **and** rejected server-side.

While LiveApply is disabled on this controller, the Apply button is disabled with the text `Live apply is not enabled on this controller.`

- **Refresh plan** calls `GET /api/v1/nodes/{id}/plan` (read-only desired-vs-actual difference; no apply audit).
- **Run audited dry-run** (operators only) calls `POST /api/v1/nodes/{id}/apply` with `{"dry_run": true}` and records `apply-dry-run`.

## API token lifecycle

Use `transitforge token list` to find token IDs and revocation status. Metadata never includes token hashes or plaintext.

```bash
transitforge token add --name edge-replacement --role agent --out-file ./edge-replacement.token
# Configure the agent with the replacement and verify it can authenticate, then:
transitforge token revoke --id OLD_TOKEN_ID
```

Revocation is permanent, audited, and blocks subsequent API requests immediately. Repeating it is safe. Only operators and administrators may revoke tokens. Revoked records remain in the database, including bootstrap records, so a controller restart does not recreate them. Before revoking your own operator token, ensure you have another working operator token or administrator account.

## Monitoring

| Process | Endpoints |
| --- | --- |
| controller | `GET /healthz` liveness, `GET /readyz` DB+init, `GET /metrics` Prometheus |
| agent | `GET /healthz`, `GET /readyz`, `GET /metrics` on `metrics_listen` (default `127.0.0.1:9101`) |

Alert rules: `deploy/prometheus/alerts.yml`.

## Docs

- `docs/ARCHITECTURE.md`
- `docs/SECURITY.md`
- `docs/OPERATIONS.md`
- `docs/FAILOVER.md`
- `docs/DOCKER.md`
- `docs/REMAINING_WORK.md`
- `docs/RENAME.md` (existing-installation migration and verification)
- `api/openapi.yaml`

## Installer (Debian/Ubuntu)

```bash
bash scripts/install.sh --role controller --listen 127.0.0.1:8443
bash scripts/install.sh --role agent --controller https://controller:8443 --token "$TOKEN" --node-name ru-edge-1
```

The installer will not overwrite an existing HAProxy config or an existing transitforge YAML/token file.

## Docker

Images: `ghcr.io/rsnest/transitforge-controller`, `ghcr.io/rsnest/transitforge-agent`, `ghcr.io/rsnest/transitforge-cli` (linux/amd64). CI publishes `sha-<short>` and `main` on pushes to `main`, plus `v*` on Git tags. Deploy with `TRANSITFORGE_VERSION=sha-…` (not `latest`).

```bash
docker compose up -d --build
docker compose --profile cli run --rm transitforge apply --node ru-edge-1 --dry-run
```

Release pull: `docs/DOCKER.md`. Live overlay: `sudo bash scripts/run-smoke-tests.sh` then `sudo bash scripts/enable-live-overlay.sh`.

