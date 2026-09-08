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
if command -v sha256sum >/dev/null 2>&1; then
	hash=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
	hash=(shasum -a 256)
else
	echo "SHA-256 verification requires sha256sum or shasum" >&2
	exit 1
fi
"${hash[@]}" --binary -- "${files[@]}" > SHA256SUMS.txt
"${hash[@]}" --check SHA256SUMS.txt
