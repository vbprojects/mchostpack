#!/usr/bin/env bash
set -euo pipefail

results_file=${1:?results file is required}

# mc-image-helper resolves /data through Hostpack's atomic symlink and can
# emit ../state/.../run.sh. The final runner starts from the real instance
# directory, where that relative path is invalid.
if [[ -f /data/run.sh ]]; then
  sed -i 's|^SERVER=.*run\.sh"$|SERVER="run.sh"|' "$results_file"
fi
