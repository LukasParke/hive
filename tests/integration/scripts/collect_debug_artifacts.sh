#!/usr/bin/env bash
set -euo pipefail

ARTIFACT_DIR="tests/integration/artifacts"
MANAGER_HOST="${DIND_MANAGER_HOST:-tcp://127.0.0.1:2375}"

mkdir -p "$ARTIFACT_DIR"

docker --host "$MANAGER_HOST" node ls >"$ARTIFACT_DIR/node-ls.txt" 2>&1 || true
docker --host "$MANAGER_HOST" service ls >"$ARTIFACT_DIR/service-ls.txt" 2>&1 || true
docker --host "$MANAGER_HOST" stack ps hive >"$ARTIFACT_DIR/stack-ps.txt" 2>&1 || true

for svc in $(docker --host "$MANAGER_HOST" service ls --format '{{.Name}}' || true); do
  docker --host "$MANAGER_HOST" service ps "$svc" >"$ARTIFACT_DIR/${svc}-ps.txt" 2>&1 || true
  docker --host "$MANAGER_HOST" service logs "$svc" >"$ARTIFACT_DIR/${svc}-logs.txt" 2>&1 || true
done
