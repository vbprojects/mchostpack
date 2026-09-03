#!/usr/bin/env bash
set -euo pipefail

install -d -o minecraft -g minecraft -m 0750 \
  /state/runtime \
  /state/runtime/tmp \
  /state/instances \
  /state/cache \
  /state/backups

export HOSTPACK_MINECRAFT_UID="$(id -u minecraft)"
export HOSTPACK_MINECRAFT_GID="$(id -g minecraft)"

# Keep the supervisor and its backup/Fly credentials separate from untrusted
# mod code. It drops only the installer/Java child to the minecraft account.
exec hostpackd "$@"
