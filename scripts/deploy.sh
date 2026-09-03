#!/usr/bin/env bash
set -euo pipefail

if ! command -v fly >/dev/null 2>&1; then
  if [[ -x "$HOME/.fly/bin/fly" ]]; then
    export PATH="$HOME/.fly/bin:$PATH"
  else
    echo "flyctl is required; install it before deploying" >&2
    exit 2
  fi
fi

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

apply_args=(
  -var="app_name=$app_name"
  -var="image_ref=$image_ref"
  -var="org_slug=$org_slug"
)

# stategraph/fly 0.2.4 acquires a Machine lease during in-place updates but
# does not forward the lease nonce to the update API call. Fly rejects that
# call with 409. Replace only the stopped Machine when its image changes; the
# app, dedicated IP, and volume remain intact.
current_image=$(tofu -chdir=infra state show fly_machine.hostpack 2>/dev/null \
  | sed -n 's/^[[:space:]]*image[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' \
  | head -1 || true)
if [[ -n "$current_image" && "$current_image" != "$image_ref" ]]; then
  echo "Machine image changed; replacing the stopped Machine to work around stategraph/fly 0.2.4 lease handling"
  apply_args+=(-replace=fly_machine.hostpack)
fi

tofu -chdir=infra apply -auto-approve "${apply_args[@]}"

# App secrets remain reported as staged for this raw (non-Fly-Launch)
# Machine. A no-op Machines API update selects the newest secret bundle while
# preserving the complete config, including any per-pack guest sizing.
machine_id=$(tofu -chdir=infra output -raw machine_id)
machine_state=""
for _ in $(seq 1 30); do
  machine_state=$(fly machines list --app "$app_name" --json \
    | python3 -c 'import json,sys; target=sys.argv[1]; print(next((m.get("state", "") for m in json.load(sys.stdin) if m.get("id") == target), ""))' "$machine_id")
  [[ "$machine_state" == "stopped" ]] && break
  sleep 2
done
if [[ "$machine_state" != "stopped" ]]; then
  echo "Machine $machine_id did not reach stopped state; staged secrets were not activated" >&2
  exit 1
fi
echo "Activating the latest staged secrets on stopped Machine $machine_id"
fly machine update "$machine_id" --app "$app_name" --skip-start --yes >/dev/null
