#!/usr/bin/env bash
set -euo pipefail

dist="${1:-dist}"
expected_commit="${2:-}"
if [[ -z "$expected_commit" ]]; then
  echo "usage: $0 [dist] <expected-commit>" >&2
  exit 2
fi

license_check="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/license-bundle-check.sh"
workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT

shopt -s nullglob
archives=("$dist"/*.tar.gz)
if [[ ${#archives[@]} -ne 4 ]]; then
  echo "release archive count = ${#archives[@]}, want 4" >&2
  exit 1
fi

for target in darwin_amd64 darwin_arm64 linux_amd64 linux_arm64; do
  matches=("$dist"/*_"$target".tar.gz)
  if [[ ${#matches[@]} -ne 1 ]]; then
    echo "release archive for $target is missing or duplicated" >&2
    exit 1
  fi
  target_dir="$workdir/$target"
  mkdir -p "$target_dir"
  tar -xzf "${matches[0]}" -C "$target_dir"
  bash "$license_check" "$target_dir"
done

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$dist" && sha256sum --check checksums.txt)
else
  (cd "$dist" && shasum --algorithm 256 --check checksums.txt)
fi

host_target="$(go env GOOS)_$(go env GOARCH)"
binary="$workdir/$host_target/metis"
if [[ ! -x "$binary" ]]; then
  echo "$host_target archive does not contain an executable metis binary" >&2
  exit 1
fi

version="$("$binary" version)"
if [[ "$version" != *"\"commit\":\"$expected_commit\""* ]]; then
  echo "packaged commit does not match $expected_commit: $version" >&2
  exit 1
fi
if [[ "$version" == *'"version":"dev"'* || "$version" == *'"version":""'* ||
      "$version" == *'"date":"unknown"'* || "$version" == *'"date":""'* ]]; then
  echo "packaged version metadata is incomplete: $version" >&2
  exit 1
fi

validation="$("$binary" validate --project release-smoke examples/demo/models/sales.ossie.yaml)"
if [[ "$validation" != *'valid: project=release-smoke'* ]]; then
  echo "packaged validation failed: $validation" >&2
  exit 1
fi

echo "Release artifacts: PASS (${host_target/_//} executed)"
