# Configuration

`config/packs.yaml` is human-authored. `config/packs.lock.json` is generated and must be committed with it.

## Pack registry

```yaml
packs:
  example-modrinth:
    display_name: Example Modrinth Pack
    provider: modrinth
    project_id: PROJECT_ID
    version_id: EXACT_VERSION_ID
    java: 21
    memory_mb: 6144
    machine_memory_mb: 8192
    machine_cpus: 4

  example-curseforge:
    display_name: Example CurseForge Pack
    provider: curseforge
    project_id: "123456"
    file_id: 7654321
    java: 17
    memory_mb: 3072
    machine_memory_mb: 4096
    machine_cpus: 2
```

IDs must be lowercase DNS labels. `example-modrinth` maps only to `example-modrinth.<domain>`. Provider, project, exact version/file, and Java become immutable when locked. A new version or second world needs a new ID.

`memory_mb` is the Java heap and is passed to the upstream image as `MEMORY`,
which sets both `-Xms` and `-Xmx`. `machine_memory_mb` and `machine_cpus` are
the Fly guest resources selected before that pack starts. Machine memory must
be a multiple of 256 MB and leave at least 256 MB outside the heap. In
practice, approximately 25% non-heap headroom is recommended.

Modrinth packs can list `modrinth_exclude_files` when a pack ships client-only
files whose metadata does not exclude dedicated servers. Entries are filename
fragments passed to the upstream Modrinth installer. For example:

```yaml
modrinth_exclude_files:
  - client-only-mod
  - another-client-file
```

The global `capacity.memory_mb` and `capacity.cpus` values are safety ceilings,
not the resources billed for every pack. For example:

```yaml
capacity:
  memory_mb: 8192
  cpus: 4
```

On Fly, a request for a differently sized pack persists `RESIZING`, updates
the same Machine through the Machines API, and asks the player to reconnect.
The attached volume is unchanged. Local development skips Fly resizing.

Run `hostpackd lock` after additions. Modrinth metadata is resolved through its public API; CurseForge requires `CF_API_KEY`. The command verifies the file belongs to the configured project and records its provider checksum and loader metadata.

## Storage

Filesystem is useful for development but is not independent of the primary Fly Volume:

```yaml
storage:
  driver: filesystem
  filesystem:
    root: /state/backups
```

S3-compatible storage reads credentials from the standard `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and optional `AWS_SESSION_TOKEN` environment variables:

```yaml
storage:
  driver: s3
  evict_after_backup: true
  s3:
    endpoint: https://t3.storage.dev
    region: auto
    bucket: my-hostpack-backups
    prefix: worlds
```

With `evict_after_backup: true`, Hostpack streams the compressed archive to
the remote backend, uploads the manifest last, reads the generation back to
verify its size and SHA-256, and only then removes the stopped pack's local
`server` directory. This keeps at most one complete pack on the Fly Volume
during a switch. The option is rejected for filesystem storage because that
would place the only remaining copy on the same volume.

Rclone reads its normal environment or configuration file. The remote includes its target directory:

```yaml
storage:
  driver: rclone
  rclone:
    remote: gdrive:hostpack/worlds
```

Every backend stores an archive followed by a manifest. A generation is ignored unless both objects exist and the archive size and SHA-256 match its manifest. Archives are streamed during backup, so no second full-size temporary archive is created on the Fly Volume.

## Runtime defaults

- Status-only wakes exit after 30 seconds and never launch Java.
- Initial login waits up to 25 seconds before asking the client to reconnect.
- Pack installation and first world generation may take up to 20 minutes before
  the launch is considered failed (`startup_timeout`).
- Another pack can switch after the active pack has been empty for two minutes.
- The Machine saves and exits after ten empty minutes.
- Backend status failures are treated as an occupied server.

The configured capacity ceilings must cover every pack. Leave volume headroom
for installation downloads, world growth, and atomic restore staging.
