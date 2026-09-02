# Fly deployment

## Prerequisites

- Fly account, organization token, `flyctl`, and a unique application name.
- OpenTofu 1.11 or newer.
- An S3-compatible, versioned state bucket created before `tofu init`.
- An owned domain where you can create a wildcard A record.
- A published Hostpack image referenced by digest.

The state bucket must not reuse the runtime Machine's storage credentials. Copy `infra/backend.hcl.example` to the ignored `infra/backend.hcl` and provide credentials through environment variables.

## Provision

```bash
tofu -chdir=infra init -backend-config=backend.hcl
tofu -chdir=infra plan \
  -var='app_name=YOUR_UNIQUE_APP' \
  -var='image_ref=ghcr.io/OWNER/REPO@sha256:DIGEST'
tofu -chdir=infra apply \
  -var='app_name=YOUR_UNIQUE_APP' \
  -var='image_ref=ghcr.io/OWNER/REPO@sha256:DIGEST'
```

Set secrets separately so their values do not enter OpenTofu state:

```bash
fly secrets set --app YOUR_UNIQUE_APP \
  RCON_PASSWORD='LONG_RANDOM_VALUE' \
  CF_API_KEY='CURSEFORGE_KEY' \
  AWS_ACCESS_KEY_ID='BACKUP_KEY' \
  AWS_SECRET_ACCESS_KEY='BACKUP_SECRET'
```

Only set the variables required by the selected storage driver. For rclone, use its supported `RCLONE_CONFIG_*` secret environment variables or a secret-backed configuration path.

Create this DNS record using the `dedicated_ipv4` output:

```text
*.mc.example.com  A  <tofu output dedicated_ipv4>
```

Start the stopped Machine once for diagnostics:

```bash
fly machine start --app YOUR_UNIQUE_APP hostpack-singleton
fly ssh console --app YOUR_UNIQUE_APP --command 'hostpackd doctor'
```

After diagnostics, let the daemon exit or stop the Machine. A status ping should wake only `hostpackd`; a login to a configured hostname should start Java.

## Deploy updates

Use the guard script with the image digest printed by the publish workflow:

```bash
./scripts/deploy.sh YOUR_UNIQUE_APP ghcr.io/OWNER/REPO@sha256:DIGEST
```

It refuses to update an active Machine. Do not use `HOSTPACK_FORCE_DEPLOY=1` while players are connected; it bypasses the clean-save protection.

Destroy the Machine before the volume if teardown is intentional. Export or verify an independent backup first.
