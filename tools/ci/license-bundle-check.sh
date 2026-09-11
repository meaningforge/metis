#!/usr/bin/env bash
set -euo pipefail
root="${1:-.}"
cd "$root"
for file in LICENSE NOTICE THIRD_PARTY_NOTICES licenses/SHA256SUMS licenses/components.json licenses/ossie/LICENSE licenses/ossie/NOTICE licenses/go/LICENSE; do
  if [[ ! -s "$file" ]]; then
    echo "Missing or empty license material: $file" >&2
    exit 1
  fi
done
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum --check licenses/SHA256SUMS >/dev/null
else
  shasum --algorithm 256 --check licenses/SHA256SUMS >/dev/null
fi
echo 'Packaged license texts: PASS'
