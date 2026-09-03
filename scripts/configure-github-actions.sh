#!/usr/bin/env bash
set -euo pipefail

repo="${1:-}"
app="${2:-${HOSTPACK_APP:-}}"

if [[ -z "$repo" ]]; then
  repo="$(gh repo view --json nameWithOwner --jq '.nameWithOwner')"
fi
if [[ -z "$app" ]]; then
  echo "usage: $0 [owner/repository] <fly-app-name>" >&2
  echo "or set HOSTPACK_APP before running it" >&2
  exit 2
fi

command -v gh >/dev/null || { echo "gh is required" >&2; exit 2; }
command -v fly >/dev/null || { echo "flyctl is required" >&2; exit 2; }

gh auth status >/dev/null
# Use the owner-authenticated flyctl session, not a token loaded from .env.
unset FLY_API_TOKEN FLY_TOKEN
fly apps show "$app" >/dev/null

echo "Setting GitHub Actions variable FLY_APP_NAME for $repo"
gh variable set FLY_APP_NAME --repo "$repo" --body "$app"

echo "Creating an app-scoped Fly deploy token (not printing its value)"
token="$(fly tokens create deploy --app "$app" --expiry 8760h)"
if [[ -z "$token" ]]; then
  echo "Fly returned an empty deploy token" >&2
  exit 1
fi
printf '%s' "$token" | gh secret set FLY_API_TOKEN --repo "$repo"
unset token

echo "Configured FLY_APP_NAME and FLY_API_TOKEN for $repo"
