#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
check=()
if [[ ${1:-} == "--check" ]]; then
  check=(--check)
fi

go run "$root/cmd/gate" generate --root "$root" "${check[@]}"
