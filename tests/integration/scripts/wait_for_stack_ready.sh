#!/usr/bin/env bash
set -euo pipefail

STACK_NAME="${STACK_NAME:-hive}"
TIMEOUT_SECONDS="${STACK_READY_TIMEOUT_SECONDS:-360}"
API_URL="${HIVE_API_BASE_URL:-http://127.0.0.1:3000/api/v1/health}"
MANAGER_HOST="${DIND_MANAGER_HOST:-tcp://127.0.0.1:2375}"

deadline=$((SECONDS + TIMEOUT_SECONDS))
attempt=0
while [ "$SECONDS" -lt "$deadline" ]; do
  attempt=$((attempt + 1))
  if ! docker --host "$MANAGER_HOST" service ls >/dev/null 2>&1; then
    sleep 2
    continue
  fi

  unhealthy_count="$(docker --host "$MANAGER_HOST" service ls --format '{{.Name}} {{.Replicas}}' | awk -v prefix="${STACK_NAME}_" '$1 ~ ("^"prefix) {split($2, r, "/"); if (r[1] != r[2]) c++} END {print c+0}')"
  if [ "$unhealthy_count" -gt 0 ]; then
    sleep 3
    continue
  fi

  if curl -fsS "$API_URL" >/dev/null 2>&1; then
    docker --host "$MANAGER_HOST" service ls --filter "name=${STACK_NAME}_"
    exit 0
  fi
  backoff=$((attempt < 10 ? attempt : 10))
  sleep "$backoff"
done

echo "Timed out waiting for stack readiness"
docker --host "$MANAGER_HOST" service ls --filter "name=${STACK_NAME}_" || true
exit 1
