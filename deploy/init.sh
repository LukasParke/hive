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
healthy=false
for i in $(seq 1 60); do
  if wget -q -O - "http://localhost:${HIVE_DIRECT_PORT}/api/v1/health" >/dev/null 2>&1 || wget -q -O - "http://localhost:${HIVE_CONTROL_PLANE_PORT}/api/v1/health" >/dev/null 2>&1; then
    echo "Hive is healthy"
    healthy=true
    break
  fi
  sleep 2
done

if [ "$healthy" != "true" ]; then
  echo "Hive did not become healthy. Docker service diagnostics:"
  docker service ls --filter label=com.docker.stack.namespace=hive || true
  docker service ps --no-trunc hive_control-plane || true
  docker service ps --no-trunc hive_traefik || true
  exit 1
fi

host_ip="$(docker info --format '{{.Swarm.NodeAddr}}' 2>/dev/null || true)"
if [ -z "$host_ip" ] || [ "$host_ip" = "0.0.0.0" ] || [ "$host_ip" = "<no value>" ]; then
  host_ip="$(ip route get 1.1.1.1 2>/dev/null | awk '{for (i=1; i<=NF; i++) if ($i == "src") {print $(i+1); exit}}' || true)"
fi
if [ -z "$host_ip" ]; then
  for ip in $(hostname -I 2>/dev/null || true); do
    case "$ip" in
      127.*|169.254.*|172.17.*|172.18.*|172.19.*|172.20.*|172.21.*|172.22.*|172.23.*|172.24.*|172.25.*|172.26.*|172.27.*|172.28.*|172.29.*|172.30.*|172.31.*) continue ;;
    esac
    host_ip="$ip"
    break
  done
fi
if [ -z "$host_ip" ]; then
  host_ip="localhost"
fi
echo "Done. Direct UI: http://${host_ip}:${HIVE_DIRECT_PORT}"
echo "Cloudflare Tunnel target: http://localhost:${HIVE_DIRECT_PORT}"
if [ -n "${HIVE_DOMAIN:-}" ]; then
  echo "Domain URL: https://${HIVE_DOMAIN}"
fi
