#!/usr/bin/env bash
set -euo pipefail

install -d -o minecraft -g minecraft -m 0750 \
  /state/runtime \
  /state/runtime/tmp \
  /state/instances \
  /state/cache \
  /state/backups

exec gosu minecraft hostpackd "$@"
