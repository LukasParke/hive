#!/usr/bin/env bash
set -euo pipefail

NETWORK_NAME="${DIND_NETWORK_NAME:-hive-dind-net}"
MANAGER_NAME="${DIND_MANAGER_NAME:-swarm-manager}"
WORKER1_NAME="${DIND_WORKER1_NAME:-swarm-worker-1}"
WORKER2_NAME="${DIND_WORKER2_NAME:-swarm-worker-2}"
MANAGER_HOST="${DIND_MANAGER_HOST:-tcp://127.0.0.1:2375}"

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

for _ in $(seq 1 30); do
  if docker --host "$MANAGER_HOST" info >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

MANAGER_IP="$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$MANAGER_NAME")"
docker --host "$MANAGER_HOST" swarm init --advertise-addr "$MANAGER_IP" >/dev/null 2>&1 || true
MANAGER_TOKEN="$(docker --host "$MANAGER_HOST" swarm join-token -q manager)"

docker exec "$WORKER1_NAME" docker swarm join --token "$MANAGER_TOKEN" "${MANAGER_IP}:2377" >/dev/null
docker exec "$WORKER2_NAME" docker swarm join --token "$MANAGER_TOKEN" "${MANAGER_IP}:2377" >/dev/null

docker --host "$MANAGER_HOST" node update --availability active "$(docker --host "$MANAGER_HOST" node ls --format '{{.ID}}' | head -n 1)" >/dev/null
docker --host "$MANAGER_HOST" node ls
