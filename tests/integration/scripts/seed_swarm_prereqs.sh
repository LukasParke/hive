#!/usr/bin/env bash
set -euo pipefail

MANAGER_HOST="${DIND_MANAGER_HOST:-tcp://127.0.0.1:2375}"

create_secret() {
  local name="$1"
  local value="$2"
  if docker --host "$MANAGER_HOST" secret ls --format '{{.Name}}' | grep -q "^${name}$"; then
    return
  fi
  printf '%s' "$value" | docker --host "$MANAGER_HOST" secret create "$name" - >/dev/null
}

create_secret "hive-master-key" "ci-master-key-01234567890123456789012345"
create_secret "postgres-password" "postgres"
create_secret "agent-bootstrap-token" "ci-bootstrap-token"

MANAGER_NODE_ID="$(docker --host "$MANAGER_HOST" info --format '{{.Swarm.NodeID}}')"
for node_id in $(docker --host "$MANAGER_HOST" node ls --format '{{.ID}}'); do
  docker --host "$MANAGER_HOST" node update \
    --label-rm ci \
    --label-rm db \
    --label-rm builder \
    --label-rm registry \
    "$node_id" >/dev/null 2>&1 || true
done
if [ -n "${MANAGER_NODE_ID}" ]; then
  docker --host "$MANAGER_HOST" node update \
    --label-add ci=true \
    --label-add db=true \
    --label-add builder=true \
    --label-add registry=true \
    "$MANAGER_NODE_ID" >/dev/null
fi

docker --host "$MANAGER_HOST" network create --driver overlay --attachable hive_internal >/dev/null 2>&1 || true
docker --host "$MANAGER_HOST" network create --driver overlay --attachable hive_proxy >/dev/null 2>&1 || true
