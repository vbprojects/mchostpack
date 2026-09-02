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
    memory_mb: 8192

  example-curseforge:
    display_name: Example CurseForge Pack
    provider: curseforge
    project_id: "123456"
    file_id: 7654321
    java: 17
    memory_mb: 10240
```

IDs must be lowercase DNS labels. `example-modrinth` maps only to `example-modrinth.<domain>`. Provider, project, exact version/file, and Java become immutable when locked. A new version or second world needs a new ID.

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
  s3:
    endpoint: https://t3.storage.dev
    region: auto
    bucket: my-hostpack-backups
    prefix: worlds
```

Rclone reads its normal environment or configuration file. The remote includes its target directory:

```yaml
storage:
  driver: rclone
  rclone:
    remote: gdrive:hostpack/worlds
```

Every backend stores an archive followed by a manifest. A generation is ignored unless both objects exist and the archive size and SHA-256 match its manifest.

## Runtime defaults

- Status-only wakes exit after 30 seconds and never launch Java.
- Initial login waits up to 25 seconds before asking the client to reconnect.
- Another pack can switch after the active pack has been empty for two minutes.
- The Machine saves and exits after ten empty minutes.
- Backend status failures are treated as an occupied server.

The configured Machine memory must be at least the largest pack memory. Leave extra volume capacity for one compressed backup and installation downloads.
