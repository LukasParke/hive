#!/usr/bin/env bash
set -euo pipefail

NETWORK_NAME="${DIND_NETWORK_NAME:-hive-dind-net}"
MANAGER_NAME="${DIND_MANAGER_NAME:-swarm-manager}"
WORKER1_NAME="${DIND_WORKER1_NAME:-swarm-worker-1}"
WORKER2_NAME="${DIND_WORKER2_NAME:-swarm-worker-2}"
MANAGER_HOST="${DIND_MANAGER_HOST:-tcp://127.0.0.1:2375}"
# Number of manager-role nodes in the cluster (manager + workers that join
# with the manager token). 3 is the historical default; the Patroni HA
# workflow sets DIND_MANAGERS=3 explicitly.
MANAGERS="${DIND_MANAGERS:-3}"
if [ "$MANAGERS" -lt 1 ] || [ "$MANAGERS" -gt 3 ]; then
  echo "DIND_MANAGERS must be between 1 and 3 (got $MANAGERS)" >&2
  exit 1
fi

cleanup_container() {
  local name="$1"
  docker rm -f "$name" >/dev/null 2>&1 || true
}

cleanup_container "$MANAGER_NAME"
cleanup_container "$WORKER1_NAME"
cleanup_container "$WORKER2_NAME"
docker network rm "$NETWORK_NAME" >/dev/null 2>&1 || true

docker network create "$NETWORK_NAME" >/dev/null

docker run -d --privileged --name "$MANAGER_NAME" \
  --network "$NETWORK_NAME" \
  --dns "${DIND_DNS_SERVER:-1.1.1.1}" \
  -p 2375:2375 \
  -p 3000:3000 \
  -p 5000:5000 \
  -p 5432:5432 \
  -p 6432:6432 \
  -e DOCKER_TLS_CERTDIR="" \
  docker:27-dind \
  --host=tcp://0.0.0.0:2375 --host=unix:///var/run/docker.sock >/dev/null

docker run -d --privileged --name "$WORKER1_NAME" \
  --network "$NETWORK_NAME" \
  --dns "${DIND_DNS_SERVER:-1.1.1.1}" \
  -e DOCKER_TLS_CERTDIR="" \
  docker:27-dind \
  --host=tcp://0.0.0.0:2375 --host=unix:///var/run/docker.sock >/dev/null

docker run -d --privileged --name "$WORKER2_NAME" \
  --network "$NETWORK_NAME" \
  --dns "${DIND_DNS_SERVER:-1.1.1.1}" \
  -e DOCKER_TLS_CERTDIR="" \
  docker:27-dind \
  --host=tcp://0.0.0.0:2375 --host=unix:///var/run/docker.sock >/dev/null

for _ in $(seq 1 60); do
  if docker --host "$MANAGER_HOST" info >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

MANAGER_IP="$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$MANAGER_NAME")"
# The dind daemon can answer `info` slightly before it is ready to seed a
# swarm; retry init instead of masking a failure with `|| true`.
swarm_ready=0
for _ in $(seq 1 30); do
  if docker --host "$MANAGER_HOST" swarm init --advertise-addr "$MANAGER_IP" >/dev/null 2>&1; then
    swarm_ready=1
    break
  fi
  sleep 2
done
if [ "$swarm_ready" -ne 1 ]; then
  echo "swarm init failed against $MANAGER_HOST" >&2
  exit 1
fi
MANAGER_TOKEN="$(docker --host "$MANAGER_HOST" swarm join-token -q manager)"
WORKER_TOKEN="$(docker --host "$MANAGER_HOST" swarm join-token -q worker)"

# Nodes join in order (manager, worker-1, worker-2); the first MANAGERS of
# them join with the manager token, the rest as plain workers.
join_node() {
  local name="$1" position="$2" token="$WORKER_TOKEN"
  if [ "$position" -le "$MANAGERS" ]; then
    token="$MANAGER_TOKEN"
  fi
  docker exec "$name" docker swarm join --token "$token" "${MANAGER_IP}:2377" >/dev/null
}
join_node "$WORKER1_NAME" 2
join_node "$WORKER2_NAME" 3

docker --host "$MANAGER_HOST" node update --availability active "$(docker --host "$MANAGER_HOST" node ls --format '{{.ID}}' | head -n 1)" >/dev/null
docker --host "$MANAGER_HOST" node ls
