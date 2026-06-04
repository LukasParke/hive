#!/usr/bin/env sh
set -eu

STACK_FILE="${STACK_FILE:-deploy/hive-stack.yml}"

echo "Checking swarm status..."
if ! docker info --format '{{.Swarm.LocalNodeState}}' | grep -q active; then
  echo "Swarm inactive, running docker swarm init"
  docker swarm init
fi

if [ -z "${HIVE_DOMAIN:-}" ]; then
  echo "HIVE_DOMAIN is required"
  exit 1
fi
if [ -z "${ACME_EMAIL:-}" ]; then
  echo "ACME_EMAIL is required"
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
  if wget -q -O - http://localhost:3000/api/v1/health >/dev/null 2>&1; then
    echo "Hive is healthy"
    break
  fi
  sleep 2
done

echo "Done. URL: https://${HIVE_DOMAIN}"
