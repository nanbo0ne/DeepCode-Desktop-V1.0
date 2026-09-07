#!/usr/bin/env bash
set -euo pipefail

cd "${1:?release directory is required}"
shopt -s nullglob
files=()
for file in *; do
  [[ -f "$file" && "$file" != SHA256SUMS.txt ]] && files+=("$file")
done
if ((${#files[@]} == 0)); then
  echo "No release files to checksum" >&2
  exit 1
fi
sha256sum -- "${files[@]}" > SHA256SUMS.txt
sha256sum --check SHA256SUMS.txt
