# Security

## Authentication

Implemented: **Bearer token + HMAC-SHA256** over `timestamp + "\n" + method + "\n" + path + "\n" + sha256(body)`.

Headers:

- `Authorization: Bearer <token>`
- `X-Proxyctl-Timestamp` (unix seconds, max skew 5m)
- `X-Proxyctl-Signature` (hex HMAC-SHA256 using the bearer token as key)

mTLS is not implemented (see remaining work).

## Tokens

Bootstrap operator token is generated once, written to a 0600 file, and stored **bcrypt-hashed** in SQLite. Operators mint additional tokens with `proxctl token add --name NAME --role agent|readonly|operator` (`POST /api/v1/tokens`). Plaintext is returned once and never stored. Live agents must use an `agent` token, not the bootstrap operator secret. WireGuard private keys are **file path references only**; `wg set … private-key <path>` is used, never a key on the command line. Logs redact obvious secret material.

## RBAC

Roles: `operator` (mutate+read), `readonly` (GET), `agent` (mutate+read). Enforced in API middleware. The optional Web UI (`--ui-listen`) uses the same tokens via `/api/v1/whoami` and an HTTP-only session cookie; UI write handlers also require `operator` before calling the API. Tokens and WireGuard private keys are never rendered in HTML.

## TLS

Controller requires TLS by default (`tls.required: true`) with optional `auto_self_signed` for labs (`tls.dns_names` extra SANs). `--plain-http` is lab-only. Existing auto-generated certs are not rewritten.

## Command execution

All host commands go through `CommandRunner` with an executable allowlist and argument arrays. There is no `sh -c`. IPs, CIDRs, ports, and interface names are validated before use.

## Rate limit

Mutating API endpoints are token-bucket limited per client IP.

## HAProxy / firewall

Managed HAProxy is confined to `# BEGIN/END PROXYCTL MANAGED`. Unrelated config is preserved. Bind collisions in unmanaged config are `CONFLICT`. Firewall uses dedicated chains and comment tags `proxyctl:mapping:<id>`; no flush of PREROUTING/FORWARD/POSTROUTING.

## Failover lock

`/run/proxyctl/transport.lock` is acquired with exclusive `flock` (LockFileEx on Windows). Acquire timeout **5s**. Metadata heartbeat TTL **30s** is informational; a live lock is never stolen. Kernel releases flock when the holder process dies.
