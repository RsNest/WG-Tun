# proxyctl

Multi-node edge/proxy infrastructure manager: desired-state controller, rootful edge agent, and `proxctl` CLI.

This software configures **your own** nodes (firewall/NAT, WireGuard, HAProxy, SSH TUN systemd units). It is not an attack tool.

## Architecture

- **controller** — TLS REST API + SQLite desired-state store (nodes, backends, mappings, tunnels, SNI routes, failover intents).
- **agent** — reconciles host state every 10s: WireGuard, iptables managed chains, HAProxy managed sections, SSH TUN unit inspection, failback state machine.
- **proxctl** — operator CLI (HMAC-signed Bearer requests).

Stages 1–5 of the staged MVP are implemented. See `docs/ARCHITECTURE.md` and `docs/REMAINING_WORK.md`.

## Build

```bash
go fmt ./...
go vet ./...
go test ./...
go build -o bin/proxyctl-controller ./cmd/controller
go build -o bin/proxyctl-agent ./cmd/agent
go build -o bin/proxctl ./cmd/proxctl
```

On Windows PowerShell:

```powershell
go fmt ./...
go vet ./...
go test ./...
go build -o bin/proxyctl-controller.exe ./cmd/controller
go build -o bin/proxyctl-agent.exe ./cmd/agent
go build -o bin/proxctl.exe ./cmd/proxctl
```

## Run locally (lab)

```powershell
# controller (plain HTTP for a local lab only)
./bin/proxyctl-controller.exe --plain-http --listen 127.0.0.1:8080 --data-dir ./data

# token is written once to ./data/bootstrap.token
$env:PROXYCTL_CONTROLLER = "http://127.0.0.1:8080"
$env:PROXYCTL_TOKEN = (Get-Content ./data/bootstrap.token).Trim()

./bin/proxctl.exe --controller http://127.0.0.1:8080 --token-file ./data/bootstrap.token --insecure node add --name ru-edge-1 --public-ip 203.0.113.10
./bin/proxctl.exe --controller http://127.0.0.1:8080 --token-file ./data/bootstrap.token --insecure backend add --name backend-a --node ru-edge-1 --address 10.200.1.2
./bin/proxctl.exe --controller http://127.0.0.1:8080 --token-file ./data/bootstrap.token --insecure mapping add --node ru-edge-1 --backend backend-a --protocol UDP --public-port 51821 --backend-port 51820
./bin/proxctl.exe --controller http://127.0.0.1:8080 --token-file ./data/bootstrap.token --insecure mapping add --node ru-edge-1 --backend backend-a --protocol TCP --public-port 443 --backend-port 443
./bin/proxctl.exe --controller http://127.0.0.1:8080 --token-file ./data/bootstrap.token --insecure apply --node ru-edge-1 --dry-run
```

Default controller listen is `127.0.0.1:8443` with TLS (`tls.auto_self_signed: true` in `configs/example-controller.yaml`). Use `--insecure` with proxctl against the lab cert.

The example agent config has `dry_run_only: true` so it will not mutate iptables/WireGuard/HAProxy until you set that to false on a real edge node.

## Web UI

Optional operator console, compiled into the controller binary (Go `html/template` + HTMX). Disabled unless you set `--ui-listen`.

```powershell
./bin/proxyctl-controller.exe --plain-http --listen 127.0.0.1:8080 --ui-listen 127.0.0.1:8444 --data-dir ./data
```

Open `http://127.0.0.1:8444`, paste an **operator** or **readonly** API token (the same bootstrap token `proxctl` uses). The token is exchanged for a 12-hour HTTP-only cookie session and is never written into HTML. Agent-role tokens cannot sign in.

Default bind is your choice at start time; **use loopback**. This is an operator tool, not a public website. If you bind it beyond localhost, put it behind the same network restrictions as the API (VPN, firewall, SSH tunnel). The UI listener is plain HTTP on purpose so it stays off the TLS API port.

Readonly users can browse; Apply / Failback / create / toggle / delete are hidden **and** rejected server-side.

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
- `api/openapi.yaml`

## Installer (Debian/Ubuntu)

```bash
bash scripts/install.sh --role controller --listen 127.0.0.1:8443
bash scripts/install.sh --role agent --controller https://controller:8443 --token "$TOKEN" --node-name ru-edge-1
```

The installer will not overwrite an existing HAProxy config or an existing proxyctl YAML/token file.

## Docker

```bash
docker compose up -d --build
docker compose --profile cli run --rm proxctl apply --node ru-edge-1 --dry-run
```

Control plane (API, SQLite, agent dry-run) runs in Compose. Live overlay on a disposable Linux VM: `sudo bash scripts/run-smoke-tests.sh` then `sudo bash scripts/enable-live-overlay.sh`. See `docs/OPERATIONS.md` and `docs/DOCKER.md`.

