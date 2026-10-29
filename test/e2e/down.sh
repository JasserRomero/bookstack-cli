#!/usr/bin/env bash
# Destroy the disposable local BookStack, its volumes and generated secrets.
set -euo pipefail

cd "$(dirname "$0")"
if [[ -f .state.local ]]; then
  set -a
  # shellcheck disable=SC1091
  source .state.local
  set +a
fi
# Compose requires the variables to be set even when tearing down.
BSK_E2E_DB_PASSWORD=${BSK_E2E_DB_PASSWORD:-unused} BSK_E2E_APP_KEY=${BSK_E2E_APP_KEY:-unused} \
  docker compose down --volumes --remove-orphans
rm -f .state.local .env.local
