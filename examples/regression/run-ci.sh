#!/usr/bin/env bash
# Reporting must never turn a failed semantic suite into a successful CI step.
set -euo pipefail
: "${METIS_BIN:?set METIS_BIN to the built Metis executable}"
: "${METIS_CI_LOG:?set METIS_CI_LOG to a fresh private log path}"
umask 077
"$METIS_BIN" semantic test "$@" 2>&1 | tee "$METIS_CI_LOG"
