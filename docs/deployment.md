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
fly secrets set --stage --app YOUR_UNIQUE_APP \
  RCON_PASSWORD='LONG_RANDOM_VALUE' \
  HOSTPACK_WEB_PASSWORD='A_DIFFERENT_LONG_RANDOM_VALUE' \
  HOSTPACK_FLY_API_TOKEN='APP_SCOPED_DEPLOY_TOKEN' \
  CF_API_KEY='CURSEFORGE_KEY' \
  AWS_ACCESS_KEY_ID='BACKUP_KEY' \
  AWS_SECRET_ACCESS_KEY='BACKUP_SECRET'
```

Create the resize token with a finite expiry and pipe it directly into the
secret command so it is not saved in shell history:

```bash
# Use the owner-authenticated flyctl session, not a limited provisioning token
# that might be loaded from .env.
unset FLY_API_TOKEN FLY_TOKEN
resize_token="$(fly tokens create deploy --app YOUR_UNIQUE_APP --expiry 8760h)"
printf 'HOSTPACK_FLY_API_TOKEN=%s\n' "$resize_token" \
  | fly secrets import --stage --app YOUR_UNIQUE_APP
unset resize_token
```

The token can manage only this Fly app, but it is still sensitive: Hostpack
uses it to read the current Machine config and update only the shared CPU and
memory fields before Java starts. The supervisor runs separately from the
unprivileged Minecraft child and removes this token and backup credentials
from the child's environment. Rotate it before expiry.

Only set the variables required by the selected storage driver. For rclone, use its supported `RCLONE_CONFIG_*` secret environment variables or a secret-backed configuration path.
`--stage` is required because Hostpack provisions a raw Fly Machine rather
than a Fly Launch release. The deployment script performs a no-op update of
the stopped Machine after OpenTofu completes, which activates the newest
secret bundle without exposing values or changing its dynamic guest size.

Create this DNS record using the `dedicated_ipv4` output:

```text
*.mc.example.com  A  <tofu output dedicated_ipv4>
```

The dashboard uses Fly's HTTPS endpoint and does not require your own domain:

```text
https://YOUR_UNIQUE_APP.fly.dev
```

Sign in as `hostpack` using `HOSTPACK_WEB_PASSWORD`. Port 80 redirects to HTTPS;
do not send dashboard credentials over plain HTTP outside local development.

## Guest page

The `Deploy guest page` GitHub Actions workflow publishes `guest/` through
GitHub Pages. In the repository's **Settings → Pages**, select **GitHub
Actions** as the source. Each main-branch push regenerates the public catalog
from `packs.yaml` and deploys it to:

```text
https://OWNER.github.io/REPOSITORY/
```

Ordinary page loads are fully static. The explicit live-status button calls
the unauthenticated, read-only `/api/guest-status` endpoint. That endpoint
returns only phase, active pack, and state-change time; it does not extend the
Machine's idle deadline. Update `STATUS_ENDPOINT` and the page CSP in
`guest/app.js` and `guest/index.html` if the Fly application name changes.

Start the stopped Machine once for diagnostics:

```bash
fly machine start --app YOUR_UNIQUE_APP hostpack-singleton
fly ssh console --app YOUR_UNIQUE_APP --command 'hostpackd doctor'
```

After diagnostics, let the daemon exit or stop the Machine. A status ping should wake only `hostpackd`; a login to a configured hostname should start Java.

## Deploy updates

Use the guard script with the image digest printed by the publish workflow:

```bash
image_ref=$(./scripts/image-ref.sh)
./scripts/deploy.sh YOUR_UNIQUE_APP "$image_ref"
```

`image-ref.sh` automatically selects the newest successful `Publish image`
workflow run. Pass `OWNER/REPO` as its first argument when running it outside
this repository.

The deployment script preserves the Fly organization slug already recorded in
OpenTofu state. For a new state, set `HOSTPACK_FLY_ORG` to the organization
slug shown by `fly orgs list`; this avoids replacement caused by Fly's
`personal` organization alias.

## Automatic image deployment

The `Publish image` workflow automatically updates the existing stopped
`hostpack-singleton` Machine after every successful push to `main`. It refuses
to update a Machine that is running, so an active server is never replaced by
CI. The existing volume, dedicated IPv4, and app are preserved.

Configure these repository settings once:

- **Actions secret** `FLY_API_TOKEN`: an app-scoped Fly deploy token.
- **Actions variable** `FLY_APP_NAME`: the Fly app name, such as `mchostpack-urz`.

Create the token locally while authenticated as the Fly owner, then add its
value under **GitHub → Settings → Secrets and variables → Actions**:

```bash
fly tokens create deploy --app YOUR_UNIQUE_APP --expiry 8760h
```

This workflow deploys the immutable commit tag produced by the image build.
It does not run OpenTofu, change secrets, resize the Machine, or deploy while
players may be connected. Use the guarded `scripts/deploy.sh` process for
infrastructure changes, secret activation, or a replacement Machine.

When an image digest changes, the script replaces only the stopped Machine and
reattaches the existing volume. This works around a lease-handling defect in
`stategraph/fly` 0.2.4's in-place Machine update path. The application,
dedicated IPv4, and volume are not replaced.

The OpenTofu Machine resource deliberately ignores later `guest` drift.
`cpus` and `memory_mb` in `infra/variables.tf` are only the bootstrap size for
a replacement Machine. Each pack's `machine_cpus` and `machine_memory_mb`
become authoritative when that pack is requested. Resizing reboots the VM, so
the first connection after selecting a differently sized pack receives a
reconnect message.

It refuses to update an active Machine. Do not use `HOSTPACK_FORCE_DEPLOY=1` while players are connected; it bypasses the clean-save protection.

Destroy the Machine before the volume if teardown is intentional. Export or verify an independent backup first.
