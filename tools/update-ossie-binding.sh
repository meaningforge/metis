#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCHEMA_DIR="$ROOT/ossie/schema"
SCHEMA_FILE="$SCHEMA_DIR/osi-schema.json"
MODEL_FILE="$ROOT/ossie/model.go"

# Explicitly pinned Apache Ossie upstream commit. Upgrading Ossie is a reviewed
# source-of-truth change: update this SHA, regenerate, inspect the schema diff,
# update NOTICE and licenses/ossie/ to match, regenerate the license bundle,
# then run the full test suite.
OSSIE_COMMIT="88e0011148283302c9a04cd0287e00e0b9d87354"
SOURCE_URL="https://raw.githubusercontent.com/apache/ossie/${OSSIE_COMMIT}/core-spec/osi-schema.json"

mkdir -p "$SCHEMA_DIR"

printf 'Fetching Apache Ossie schema at %s\n' "$OSSIE_COMMIT"
curl --fail --silent --show-error --location "$SOURCE_URL" --output "$SCHEMA_FILE"

version="$(sed -n 's/.*"const": "\([^"]*\)".*/\1/p' "$SCHEMA_FILE" | head -1)"
if [[ -z "$version" ]]; then
  echo "failed to determine Ossie schema version" >&2
  exit 1
fi

printf 'Generating Go binding for Apache Ossie schema version %s\n' "$version"
go run ./cmd/ossiegen -schema "$SCHEMA_FILE" -out "$MODEL_FILE" -upstream-revision "$OSSIE_COMMIT"
gofmt -w "$MODEL_FILE"

printf 'Ossie binding regenerated successfully.\n'
