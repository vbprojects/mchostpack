#!/usr/bin/env bash
set -euo pipefail

repo=${1:-}
if [[ -z "$repo" ]]; then
  repo=$(gh repo view --json nameWithOwner --jq '.nameWithOwner')
fi

run_id=$(gh run list \
  --repo "$repo" \
  --workflow build.yml \
  --status success \
  --limit 1 \
  --json databaseId \
  --jq '.[0].databaseId')

if [[ -z "$run_id" || "$run_id" == "null" ]]; then
  echo "no successful Publish image workflow run found for $repo" >&2
  exit 1
fi

image_ref=$(gh run view "$run_id" --repo "$repo" --log \
  | grep -oE 'ghcr\.io/[^[:space:]]+@sha256:[0-9a-f]{64}' \
  | tail -1 || true)

if [[ -z "$image_ref" ]]; then
  echo "workflow run $run_id succeeded but its image digest was not found" >&2
  echo "inspect it with: gh run view $run_id --repo $repo --log" >&2
  exit 1
fi

printf '%s\n' "$image_ref"
