# Architecture

`docs/ARCHITECTURE.md` is the canonical architectural contract for this repository.
`api/openapi.yaml` is the machine-readable API specification and MUST stay synchronized with it.

Any new or changed HTTP endpoint must update, in the same change:

- router/handler implementation
- `internal/client` where applicable
- tests
- `api/openapi.yaml`
- this endpoint table

Do not create a second architecture document at the repository root.

proxyctl is a desired-state manager for the operator's own edge nodes.

```
proxctl  -->  controller (TLS API, SQLite)
                 ^
                 | HMAC + Bearer
                 v
    edge-agent (root)
                 |-- WireGuard (ip/wg, never wg-quick)
                 |-- iptables managed chains PROXYCTL_{DNAT,FORWARD,SNAT}
                 |-- HAProxy managed section + `haproxy -c`; `systemctl reload` or host watcher (`haproxy_reload`)
                 `-- SSH TUN systemd unit inspect/start (not an SSH implementation)
```

## Reconciliation

```
desired := controller.GetNodeConfig()
actual  := Discover()
plan    := Diff(desired, actual)
if plan empty: return
validate; backup; apply; verify; else rollback
```

Plans are `ADD:` / `CHANGE:` / `DELETE:` lines. Applying an unchanged desired state a second time yields `NO CHANGES` (no HAProxy reload, no extra iptables rules, no WG recreate). Unexpected host state is `CONFLICT`, never a silent overwrite.

`GET /api/v1/nodes/{id}/plan` and `POST /api/v1/nodes/{id}/apply` both call the same `reconcile.Diff` implementation. They are not the same operation:

| | `GET .../plan` | `POST .../apply` with `{"dry_run": true}` |
| --- | --- | --- |
| Role | read (readonly, operator, agent) | write (operator, agent) |
| Mutates inventory | no | no |
| Audit | none | `apply-dry-run` |
| Purpose | pure plan preview | explicit audited dry-run request |

Live apply on this controller is disabled (`Capabilities.LiveApply == false`). `POST .../apply` with `dry_run: false` still returns a dry-run plan and an `apply-dry-run` audit event; host mutation remains agent-driven.

## Mapping enabled

`PortMapping.Enabled` is an intentional desired-state rule:

| `enabled` | Inventory | Desired-state | Reconcile plan |
| --- | --- | --- | --- |
| `true` | listed | included | ADD/CHANGE as needed |
| `false` | listed (catalog) | omitted | DELETE the managed firewall rule if it still exists on the node |

Unchanged disabled state after that DELETE has converged is `NO CHANGES`.

## Actual-state

| Method | Path | Role |
| --- | --- | --- |
| `POST` | `/api/v1/nodes/{id}/actual-state` | agent (write): report discovered state |
| `GET` | `/api/v1/nodes/{id}/actual-state` | read: last stored `ActualState` plus `AgentStatus` (including `TransportState` inside actual) |

GET does not expose secrets (API tokens, bootstrap tokens, HMAC secrets, WireGuard/SSH private keys, or one-time mint values).

## Failback

Externally the operation is **Failback**: the explicit human-triggered transition `SSH_PRIMARY` → `WG_PRIMARY`.

`POST /api/v1/nodes/{id}/failback` records that request and returns **202** with the stored intent. Internal persistence may still use the symbol `fail_forward`; that is an implementation detail, not the user-facing name.

## REST API endpoints

Authentication: Bearer token plus HMAC-SHA256 over `timestamp + method + path + sha256(body)` using the bearer token as key.

RBAC is enforced on the REST API. The optional Web UI adds a second session gate (cookie) in front of the same API.

| Method | Path | Auth | Purpose |
| --- | --- | --- | --- |
| GET | `/healthz` | none | process liveness |
| GET | `/readyz` | none | SQLite reachable and server initialized |
| GET | `/metrics` | none | Prometheus exposition (501 if metrics disabled) |
| GET | `/api/v1/whoami` | read | caller name and role |
| GET | `/api/v1/tokens` | operator | token metadata (hashes omitted) |
| POST | `/api/v1/tokens` | operator | mint a token (plaintext returned once) |
| GET | `/api/v1/nodes` | read | list nodes |
| POST | `/api/v1/nodes` | write | create node |
| GET | `/api/v1/nodes/{id}` | read | get node |
| GET | `/api/v1/nodes/{id}/desired-state` | read | desired-state (disabled mappings omitted) |
| GET | `/api/v1/nodes/{id}/actual-state` | read | last stored actual-state + agent status |
| POST | `/api/v1/nodes/{id}/actual-state` | write | agent reports actual-state |
| GET | `/api/v1/nodes/{id}/plan` | read | read-only Diff plan; no apply audit |
| POST | `/api/v1/nodes/{id}/apply` | write | explicit apply; `dry_run: true` is audited |
| POST | `/api/v1/nodes/{id}/failback` | write | Failback (`SSH_PRIMARY` → `WG_PRIMARY`); 202 |
| GET | `/api/v1/backends` | read | list backends |
| POST | `/api/v1/backends` | write | create backend |
| GET | `/api/v1/backends/{id}` | read | get backend |
| PATCH | `/api/v1/backends/{id}` | write | update backend |
| GET | `/api/v1/mappings` | read | list mappings (including disabled) |
| POST | `/api/v1/mappings` | write | create mapping (`enabled` defaults true) |
| PATCH | `/api/v1/mappings/{id}` | write | update mapping fields, including `enabled` |
| DELETE | `/api/v1/mappings/{id}` | write | delete mapping |
| GET | `/api/v1/tunnels` | read | list tunnels |
| POST | `/api/v1/tunnels` | write | create tunnel |
| GET | `/api/v1/tunnels/{id}/status` | read | tunnel status from last actual-state |
| GET | `/api/v1/sni-routes` | read | list SNI routes |
| POST | `/api/v1/sni-routes` | write | create SNI route |
| GET | `/api/v1/sni-routes/{id}` | read | get SNI route |
| PATCH | `/api/v1/sni-routes/{id}` | write | update SNI route |
| GET | `/api/v1/events` | read | audit log; query `node`, `backend`, `since`, `until`, `action` |

**Read** = `readonly`, `operator`, `agent`. **Write** = `operator`, `agent`. Token mint/list = `operator` only.

Agent tokens are not accepted as Web UI logins. Agent APIs remain the same node-unbound REST surface as today except where a handler already scopes by path `{id}`; do not treat agent as a general operator.

`since` / `until` accept `YYYY-MM-DD` or RFC3339. There are no `from` / `to` aliases. `action` is an exact match on the audit action name.

Not registered (removed, no aliases):

- `PUT /api/v1/backends/{id}`
- `PUT /api/v1/mappings/{id}`
- `GET /api/v1/mappings/{id}`
- `PUT /api/v1/sni-routes/{id}`
- `GET /api/v1/audit`

## Packages

| Package | Role |
| --- | --- |
| `internal/model` | shared structs |
| `internal/store` | SQLite + migrations |
| `internal/api` | REST + rate limit + RBAC |
| `internal/webui` | optional operator UI (html/template + HTMX), talks to the REST API in-process |
| `internal/engine` | Discover/Apply/Rollback orchestration |
| `internal/firewall` | iptables-nft manager |
| `internal/wireguard` | interface/peer manager |
| `internal/haproxy` | marker-section templating |
| `internal/sshtun` | systemd unit inspect/start |
| `internal/failover` | state machine + flock + transport-state.json |
| `internal/health` | overlay ping + probes |
| `internal/cmdexec` | argument-array CommandRunner |

## Example topology (fixtures only)

See `configs/example-topology.yaml`. Those IPs/ports are not hardcoded in `internal/`.

## Web UI

The operator console is compiled into `proxyctl-controller` (`internal/webui`) and is **off unless** you pass `--ui-listen`. It is a **separate HTTP listener** (default suggestion `127.0.0.1:8444`), not a path prefix on the TLS API.

Why a second listener: the API mux is Bearer + HMAC. Cookie sessions and HTML forms do not belong on that surface. A localhost HTTP UI avoids mixing `Set-Cookie` with the existing TLS authenticator and needs no change to agent clients.

The UI does not implement inventory or reconcile itself. Handlers call `internal/client` through an in-process `http.RoundTripper` that invokes `api.Handler().ServeHTTP`. Node detail and the Plan preview button obtain the plan from `GET /api/v1/nodes/{id}/plan`. The UI must not import `internal/reconcile`.

Writes require an **operator** session on the UI **and** still pass API RBAC. Readonly sessions can view inventory, plan, status, and events; POST/PATCH/DELETE from the UI are rejected with 403 before the API is called.

While `LiveApply` is false, the Apply control is disabled and the page shows: "Live apply is not enabled on this controller." Plan preview remains available. The UI does not call `POST .../apply {"dry_run":true}` for preview; that remains a distinct audited API/CLI operation.

HTML `GET /events` is the operator page. It calls `GET /api/v1/events` with `node`, `backend`, `since`, `until`, and `action`.

Vendored assets (no CDN): HTMX 2.0.4 (`0BSD`) and a handwritten `app.css`.

## Build and distribution

```
GitHub
  -> GitHub Actions
  -> Docker Buildx (linux/amd64)
  -> GHCR (ghcr.io/rsnest/wg-tun-{controller,agent,proxctl})
  -> explicit operator deployment (`PROXYCTL_VERSION=sha-…` or `v…`)
```

Production and lab runtime hosts **pull images**. They do not compile Go. Native host/systemd integration remains required for HAProxy reload and SSH TUN units; the agent container does not mount the systemd control socket and does not replace those host units. See `docs/DOCKER.md`.

