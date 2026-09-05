# TransitForge rename

The implemented API contract remains `ARCHITECTURE.md`. `ROADMAP.md` describes
future work. Foreign Nodes and provider integrations are not part of this rename.

## Names

| Surface | Current name |
| --- | --- |
| Product, sidebar, page titles | TransitForge |
| Login | Sign in to TransitForge / Вход в TransitForge |
| CLI / Go module | `transitforge` |
| Controller / agent binaries | `transitforge-controller`, `transitforge-agent` |
| Local images | `transitforge-controller:local`, `transitforge-agent:local`, `transitforge-cli:local` |
| Published images | `ghcr.io/rsnest/transitforge-controller`, `ghcr.io/rsnest/transitforge-agent`, `ghcr.io/rsnest/transitforge-cli` |
| Repository / image source label | `https://github.com/RsNest/TransitForge` |
| Environment / metrics | `TRANSITFORGE_*` / `transitforge_*` |
| Config / installed data | `/etc/transitforge`, `/var/lib/transitforge` |
| UI cookies | `transitforge_ui`, `transitforge_locale`, `transitforge_nav` |
| New TOTP enrollment issuer | TransitForge |
| SQLite filename | `transitforge.db` |

Publication uses `sha-<short>`, `main`, and version tags with `latest=false`.
Historical packages are retained. Repository renaming precedes changing `origin`.

## Existing installations

1. Stop every controller using the data directory. Back up the **entire directory**
   offline, including the database, journal/WAL/SHM, bootstrap token and certificates.
2. Retain the existing data mount. Changing a Compose project/volume name can create
   an empty volume: explicitly mount the original volume or bind directory.
3. Start the renamed controller. The migration renames `proxyctl.db` and any
   `-wal`, `-shm`, `-journal` files; it does not recreate the database or reset users.
   Ambiguous old/new files, conflicting destination sidecars, nonregular paths and
   failed renames stop startup. Rename failures roll back completed file moves.
4. Verify `/readyz`, inventory and login before discarding any rollback resources.
   A power loss during the multi-file rename requires offline inspection/restoration
   from backup; the filesystem does not provide an atomic multi-file rename.

Passwords, stored TOTP secrets, enabled MFA state and recovery codes are unchanged.
Existing authenticator entries continue producing valid codes under their old
display label. Cookies/process-local sessions expire during the update; sign in again.
Existing TLS certificates are retained. A password reset is a separate owner action.

Legacy strings in `internal/config` and its tests are filename migration fixtures.
Legacy iptables chain/comment names and HAProxy markers/digests remain read-compatible
in `internal/firewall` and `internal/haproxy` so existing managed host state is
recognized. New generated state uses TransitForge names. Do not remove compatibility
literals as if they were branding leftovers. Live host migration remains subject
to dry-run review and the existing Linux smoke gate.

## Verification

```bash
docker build --target test .
docker buildx bake controller agent cli
CONTROLLER_IMAGE=transitforge-controller:local \
AGENT_IMAGE=transitforge-agent:local \
CLI_IMAGE=transitforge-cli:local bash scripts/docker-ci-smoke.sh
```

The smoke script supports Linux and Git Bash with Docker Desktop. It checks binary
names, packaged configuration, controller/agent health, runtime tools, process users
and exclusion of checkout secrets. It uses disposable containers, not the demo.

The local demo remains at `http://127.0.0.1:8444` (API `http://127.0.0.1:8080`),
in `transitforge-demo-controller`, using its original data mount. At rename validation,
all rows across all 14 existing SQLite tables matched the offline backup, including
one node, one backend, one tunnel, two mappings, one SNI route and one human user;
SQLite `integrity_check` returned `ok`. Both login locales were checked in-browser.

The final delivery report must also identify the pushed SHA, successful `validate`
and `publish-images` runs, and the three GHCR images for that SHA. Local build/smoke
success alone does not close the publication gate. Phase 1 remains gated until that
report is complete.
