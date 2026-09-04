# Remaining work

Stages 1–5 of the staged MVP are in tree. The items below are intentionally not pretended to work.

## Known gaps

- Postgres store implementation (interface is SQLite-only today).
- mTLS agent↔controller (Bearer+HMAC is implemented instead).
- Token revoke/rotate API and per-node binding (operator can mint `agent` tokens via `POST /api/v1/tokens` / `proxctl token add`; revoke is still missing). Do not reuse the bootstrap operator token on live agents.
- UDP backend probes (TCP probes + overlay ping only).
- HAProxy SNI backend port is assumed 443 when rendering `server` lines.
- `automatic_failforward` cannot enable unattended SSH→WG (by design). The Web UI can record the same operator fail-forward intent as the CLI.
- Controller does not push apply to agents (agents poll).
- No combined “new backend + tunnel + mappings” wizard; use the existing sequential API/CLI (or the separate forms in the Web UI).
- Installer does not install WireGuard/HAProxy packages; it only checks a subset and refuses to overwrite HAProxy config.
- Windows is a build/test host; the agent’s live path is Linux (`ip`, `wg`, `iptables`, `systemctl`).
- Docker Compose runs the control plane. Live iptables/WG requires Linux + `network_mode: host` on the agent (`docker-compose.live.yml`). Host HAProxy reload is a path unit (`haproxy_reload: external`); SSH TUN systemd units are not started from the container. Native systemd agent is the production layout. Gate live overlay with `scripts/run-smoke-tests.sh` then `scripts/enable-live-overlay.sh`.

## Safest next step

Issue an `agent`-role token, point `example-agent.yaml` at it, run the agent with `dry_run_only: true` on a lab VM, and review plans before setting `dry_run_only: false`.
