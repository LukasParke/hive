#!/usr/bin/env sh
set -eu

STACK_FILE="${STACK_FILE:-deploy/hive-stack.yml}"
HIVE_DIRECT_PORT="${HIVE_DIRECT_PORT:-8080}"
HIVE_CONTROL_PLANE_PORT="${HIVE_CONTROL_PLANE_PORT:-3000}"

echo "Checking swarm status..."
if ! docker info --format '{{.Swarm.LocalNodeState}}' | grep -q active; then
  echo "Swarm inactive, running docker swarm init"
  docker swarm init
fi

if [ -z "${HIVE_DOMAIN:-}" ]; then
  echo "HIVE_DOMAIN is not set; Hive will be available on the direct HTTP port only."
  ACME_EMAIL="${ACME_EMAIL:-admin@example.invalid}"
  export ACME_EMAIL
elif [ -z "${ACME_EMAIL:-}" ]; then
  echo "ACME_EMAIL is required when HIVE_DOMAIN is set"
  exit 1
fi

create_secret() {
  name="$1"
  if ! docker secret ls --format '{{.Name}}' | grep -q "^${name}$"; then
    value="$(openssl rand -hex 32)"
    printf "%s" "$value" | docker secret create "$name" -
  fi
}

create_secret hive-master-key
create_secret postgres-password
create_secret agent-bootstrap-token

docker node update --label-add db=true "$(docker node ls --format '{{.ID}}' | head -n 1)"
docker node update --label-add builder=true "$(docker node ls --format '{{.ID}}' | head -n 1)"
docker node update --label-add registry=true "$(docker node ls --format '{{.ID}}' | head -n 1)"

echo "Deploying stack (image tag: ${HIVE_IMAGE_TAG:-latest})..."
docker stack deploy -c "$STACK_FILE" hive

echo "Waiting for API health..."
for i in $(seq 1 60); do
  if wget -q -O - "http://localhost:${HIVE_DIRECT_PORT}/api/v1/health" >/dev/null 2>&1 || wget -q -O - "http://localhost:${HIVE_CONTROL_PLANE_PORT}/api/v1/health" >/dev/null 2>&1; then
    echo "Hive is healthy"
    break
  fi
  sleep 2
done

host_ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
if [ -z "$host_ip" ]; then
  host_ip="localhost"
fi
echo "Done. Direct UI: http://${host_ip}:${HIVE_DIRECT_PORT}"
echo "Cloudflare Tunnel target: http://localhost:${HIVE_DIRECT_PORT}"
if [ -n "${HIVE_DOMAIN:-}" ]; then
  echo "Domain URL: https://${HIVE_DOMAIN}"
fi
