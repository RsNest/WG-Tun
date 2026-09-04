# Architecture

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

The UI does not implement inventory or reconcile itself. Handlers call `internal/client` through an in-process `http.RoundTripper` that invokes `api.Handler().ServeHTTP`. Writes require an **operator** session on the UI **and** still pass API RBAC. Readonly sessions can view pages; POST/PATCH/DELETE from the UI are rejected with 403 before the API is called.

Vendored assets (no CDN): HTMX 2.0.4 (`0BSD`) and a handwritten `app.css`.

