#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <fly-app-name> <image@sha256:digest>" >&2
  exit 2
fi

app_name=$1
image_ref=$2
if [[ ! "$image_ref" =~ @sha256:[0-9a-f]{64}$ ]]; then
  echo "image must be pinned by sha256 digest" >&2
  exit 2
fi

# Fly expands the special "personal" organization alias to the account's real
# organization slug. Reusing "personal" on a later apply makes provider 0.2.4
# see an organization change and replace the entire app (including its volume).
# Preserve the normalized slug from state unless the operator sets one.
org_slug=${HOSTPACK_FLY_ORG:-}
if [[ -z "$org_slug" ]]; then
  org_slug=$(tofu -chdir=infra state show fly_app.hostpack 2>/dev/null \
    | sed -n 's/^[[:space:]]*org_slug[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' \
    | head -1 || true)
fi
if [[ -z "$org_slug" || "$org_slug" == "personal" ]]; then
  echo "set HOSTPACK_FLY_ORG to your actual Fly organization slug (not 'personal')" >&2
  echo "find it with: fly orgs list" >&2
  exit 2
fi

machine_json=$(fly machines list --app "$app_name" --json 2>/dev/null || true)
if [[ -n "$machine_json" ]]; then
  running=$(python3 -c 'import json,sys; print(any(m.get("state") not in ("stopped", "destroyed") for m in json.load(sys.stdin)))' <<<"$machine_json")
  if [[ "$running" == "True" && "${HOSTPACK_FORCE_DEPLOY:-}" != "1" ]]; then
    echo "refusing to deploy while a Machine is active; wait for clean idle shutdown" >&2
    exit 1
  fi
fi

tofu -chdir=infra init -backend-config=backend.hcl
tofu -chdir=infra apply \
  -var="app_name=$app_name" \
  -var="image_ref=$image_ref" \
  -var="org_slug=$org_slug"
