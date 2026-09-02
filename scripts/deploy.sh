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

machine_json=$(fly machines list --app "$app_name" --json 2>/dev/null || true)
if [[ -n "$machine_json" ]]; then
  running=$(python3 -c 'import json,sys; print(any(m.get("state") not in ("stopped", "destroyed") for m in json.load(sys.stdin)))' <<<"$machine_json")
  if [[ "$running" == "True" && "${HOSTPACK_FORCE_DEPLOY:-}" != "1" ]]; then
    echo "refusing to deploy while a Machine is active; wait for clean idle shutdown" >&2
    exit 1
  fi
fi

tofu -chdir=infra init -backend-config=backend.hcl
tofu -chdir=infra apply -var="app_name=$app_name" -var="image_ref=$image_ref"
