#!/usr/bin/env bash
# Reporting must never turn a failed semantic suite into a successful CI step.
set -euo pipefail
: "${METIS_BIN:?set METIS_BIN to the built Metis executable}"
: "${METIS_CI_LOG:?set METIS_CI_LOG to a fresh private log path}"
umask 077
# Refuse an existing file or symlink before executing the suite. Keep the
# exclusively created descriptor open so tee never reopens a caller's path.
if [[ -e "$METIS_CI_LOG" || -L "$METIS_CI_LOG" ]]; then
  printf '%s\n' 'semantic CI console log already exists' >&2
  exit 2
fi
set -o noclobber
exec 3> "$METIS_CI_LOG"
"$METIS_BIN" semantic test "$@" 2>&1 | tee /dev/fd/3
