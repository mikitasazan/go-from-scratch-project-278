#!/usr/bin/env bash
set -euo pipefail

echo "[run.sh] Starting service"

echo "[run.sh] Running DB migrations"
goose -dir ./db/migrations postgres "${DATABASE_URL}" up

echo "[run.sh] Starting Go app"
exec /app/bin/app
