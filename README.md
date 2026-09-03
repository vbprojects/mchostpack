# Hostpack

Hostpack runs one of several pinned Minecraft Java modpacks on one Fly Machine. Players choose a pack with a hostname such as `atm9.mc.example.com`; only one pack and world can be active at a time. The Machine exits after a clean idle save so Fly Proxy can wake the same Machine on the next connection.

This repository is an MVP intended for a small, operator-managed deployment. It has a read-only authenticated status dashboard, but no mutation API or database.

## What is implemented

- Exact hostname routing for Minecraft status and login handshakes.
- Synthetic sleeping, loading, busy, and failure status without starting Java.
- Persisted singleton lifecycle with crash recovery and concurrent-start protection.
- Per-pack Fly Machine RAM/CPU sizing before Java starts.
- Modrinth and CurseForge exact-version locking.
- Java 17/21 selection over the `itzg/minecraft-server` installer and launcher.
- RCON `save-all flush` and clean stop before switching or exiting.
- Immutable generation backups through filesystem, S3-compatible, or rclone storage.
- Optional verified remote eviction keeps at most one complete pack on the volume.
- OpenTofu provisioning for one Fly app, dedicated IPv4, volume, and stopped Machine.
- Manual, digest-pinned deployment guarded against replacing an active Machine.
- Authenticated web status, resource summaries, and bounded recent logs.

## Quick local validation

Install a current Go toolchain, then run:

```bash
go test ./...
go run ./cmd/hostpackd config validate \
  --config config/packs.yaml \
  --lock config/packs.lock.json \
  --state ./state
tofu -chdir=infra init -backend=false
tofu -chdir=infra validate
```

To run a real pack locally, add it to `config/packs.yaml`, generate the lock, set the required secrets in `.env`, and use `docker compose up --build`. First installation can take several minutes.

```bash
go run ./cmd/hostpackd lock \
  --config config/packs.yaml \
  --lock config/packs.lock.json
docker compose up --build
```

The local dashboard is available at `http://localhost:8080`. Its default local
credentials are `hostpack` / `local-dashboard-only`; set
`HOSTPACK_WEB_PASSWORD` in `.env` before sharing the port.

CurseForge lock generation requires `CF_API_KEY`. Never commit `.env`, rclone configuration, RCON passwords, or object-storage credentials.

## Repository map

- `cmd/hostpackd` contains the operator CLI and daemon entry point.
- `internal` contains configuration, Minecraft protocol, lifecycle, RCON, and backup implementations.
- `config` is the authoritative registry and its generated immutable lock.
- `infra` provisions the fixed Fly topology.
- `docs` contains configuration, deployment, and recovery runbooks.

## Important limitations

- Public login is enabled. Anyone who knows a configured hostname can start its pack, consume compute, or switch it after the empty grace period.
- A single Fly Volume is a single-host primary copy. Configure S3 or rclone for independent backups before using valuable worlds.
- Pack IDs cannot be changed or removed. Add a new ID for a new pack version or world.
- Packs requiring manual browser downloads are unsupported by the unattended MVP.
- Per-pack resizing requires an app-scoped Fly deploy token in the
  `HOSTPACK_FLY_API_TOKEN` app secret. A resize reboots the Machine, so the
  triggering player must reconnect.
- The default image arguments use upstream tags for developer convenience. Production CI should pass immutable image digests for all three base images.

See [deployment](docs/deployment.md), [configuration](docs/configuration.md), and [operations](docs/operations.md) before exposing a server.
