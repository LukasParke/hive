#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# ── Load config ────────────────────────────────────────────────────────
ENV_FILE="$PROJECT_DIR/deploy.env"
if [ ! -f "$ENV_FILE" ]; then
  echo "ERROR: deploy.env not found. Copy deploy.env.example and configure it:"
  echo "  cp deploy.env.example deploy.env"
  exit 1
fi
# shellcheck source=/dev/null
source "$ENV_FILE"

REMOTE_HOST="${REMOTE_HOST:?REMOTE_HOST not set in deploy.env}"
REMOTE_USER="${REMOTE_USER:?REMOTE_USER not set in deploy.env}"
REMOTE_DIR="${REMOTE_DIR:-~/hive-dev}"
REGISTRY="${REGISTRY:-127.0.0.1:5000}"
IMAGE="${IMAGE:-hive}"
HIVE_URL="${HIVE_URL:-http://${REMOTE_HOST}}"

SSH_TARGET="${REMOTE_USER}@${REMOTE_HOST}"
FULL_IMAGE="${REGISTRY}/${IMAGE}:latest"

# ── Parse flags ────────────────────────────────────────────────────────
DOCKER_BUILD_FLAGS=""
UPDATE_SERVICES=("hive-manager" "hive-engine" "hive-agent")
SKIP_BUILD=false

for arg in "$@"; do
  case "$arg" in
    --no-cache)
      DOCKER_BUILD_FLAGS="--no-cache"
      ;;
    --service=*)
      svc="${arg#--service=}"
      if [[ "$svc" != hive-* ]]; then
        svc="hive-${svc}"
      fi
      UPDATE_SERVICES=("$svc")
      ;;
    --restart-only)
      SKIP_BUILD=true
      ;;
    --help|-h)
      echo "Usage: $0 [OPTIONS]"
      echo ""
      echo "Options:"
      echo "  --no-cache        Build Docker image without cache"
      echo "  --service=NAME    Only update a specific service (e.g., --service=manager)"
      echo "  --restart-only    Skip build, just force-restart services"
      echo "  -h, --help        Show this help"
      exit 0
      ;;
    *)
      echo "Unknown flag: $arg (use --help for usage)"
      exit 1
      ;;
  esac
done

# ── Helpers ────────────────────────────────────────────────────────────
step_start() {
  STEP_START=$(date +%s)
  echo ""
  echo "━━━ $1 ━━━"
}

step_done() {
  local elapsed=$(( $(date +%s) - STEP_START ))
  echo "    done (${elapsed}s)"
}

TOTAL_START=$(date +%s)

# ── Sync source ────────────────────────────────────────────────────────
if [ "$SKIP_BUILD" = false ]; then
  step_start "Syncing source to ${SSH_TARGET}:${REMOTE_DIR}"
  rsync -az --delete \
    --exclude '.git' \
    --exclude 'bin/' \
    --exclude 'ui/node_modules' \
    --exclude 'ui/.svelte-kit' \
    --exclude 'ui/build' \
    --exclude 'ui/dist' \
    --exclude 'deploy.env' \
    --exclude '.env' \
    --exclude '.cursor/' \
    --exclude 'coverage.*' \
    --exclude '*.out' \
    -e "ssh -o StrictHostKeyChecking=no -o ConnectTimeout=5" \
    "$PROJECT_DIR/" "${SSH_TARGET}:${REMOTE_DIR}/"
  step_done

  # ── Build image on remote ──────────────────────────────────────────
  step_start "Building Docker image on remote"
  # shellcheck disable=SC2029
  ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
    "cd ${REMOTE_DIR} && docker buildx build ${DOCKER_BUILD_FLAGS} --push -t ${FULL_IMAGE} ."
  step_done
fi

# ── Deploy monitoring stack ────────────────────────────────────────────
step_start "Ensuring monitoring stack (Prometheus, node-exporter, cAdvisor)"

# Deploy node-exporter (global)
# shellcheck disable=SC2029
ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" "docker service inspect hive-node-exporter >/dev/null 2>&1 || \
  docker service create --name hive-node-exporter \
    --mode global \
    --network ${HIVE_NETWORK:-hive-net} \
    --label hive.managed=true \
    --mount type=bind,src=/proc,dst=/host/proc,readonly \
    --mount type=bind,src=/sys,dst=/host/sys,readonly \
    --mount type=bind,src=/,dst=/rootfs,readonly \
    --publish mode=host,target=9100,published=9100 \
    prom/node-exporter:latest \
    --path.procfs=/host/proc --path.sysfs=/host/sys --path.rootfs=/rootfs \
    '--collector.filesystem.mount-points-exclude=^/(sys|proc|dev|host|etc)(\$\$|/)'" \
  && echo "  node-exporter: ok" || echo "  node-exporter: already running or failed"

# Deploy cAdvisor (global)
# shellcheck disable=SC2029
ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" "docker service inspect hive-cadvisor >/dev/null 2>&1 || \
  docker service create --name hive-cadvisor \
    --mode global \
    --network ${HIVE_NETWORK:-hive-net} \
    --label hive.managed=true \
    --mount type=bind,src=/,dst=/rootfs,readonly \
    --mount type=bind,src=/var/run,dst=/var/run \
    --mount type=bind,src=/sys,dst=/sys,readonly \
    --mount type=bind,src=/var/lib/docker/,dst=/var/lib/docker,readonly \
    --publish mode=host,target=8080,published=8180 \
    gcr.io/cadvisor/cadvisor:latest" \
  && echo "  cadvisor: ok" || echo "  cadvisor: already running or failed"

# Create/update Prometheus config
PROM_CONFIG_FILE="${REMOTE_DIR}/config/prometheus.yml"
# shellcheck disable=SC2029
EXISTING_CONFIG=$(ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
  "docker config ls --format '{{.Name}}' | grep '^hive-prom-config' | tail -1" 2>/dev/null || true)

PROM_CONFIG_NAME="hive-prom-config-$(date +%s)"
# shellcheck disable=SC2029
ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
  "docker config create ${PROM_CONFIG_NAME} ${PROM_CONFIG_FILE}" \
  && echo "  prometheus config: created ${PROM_CONFIG_NAME}" || echo "  prometheus config: failed"

# Deploy or update Prometheus
# shellcheck disable=SC2029
if ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" "docker service inspect hive-prometheus >/dev/null 2>&1"; then
  echo "  prometheus: updating config and ensuring root user"
  # shellcheck disable=SC2029
  ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" "docker service update \
    --config-rm \$(docker service inspect hive-prometheus --format '{{range .Spec.TaskTemplate.ContainerSpec.Configs}}{{.ConfigName}} {{end}}' | tr ' ' '\n' | head -1) \
    --config-add source=${PROM_CONFIG_NAME},target=/etc/prometheus/prometheus.yml \
    --user 0 \
    --force hive-prometheus" 2>/dev/null || \
  echo "  prometheus: config update skipped (manual refresh may be needed)"
else
  # shellcheck disable=SC2029
  ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" "docker service create --name hive-prometheus \
    --network ${HIVE_NETWORK:-hive-net} \
    --label hive.managed=true \
    --constraint node.role==manager \
    --user 0 \
    --mount type=volume,src=hive-prometheus-data,dst=/prometheus \
    --mount type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock,readonly \
    --config source=${PROM_CONFIG_NAME},target=/etc/prometheus/prometheus.yml \
    prom/prometheus:latest \
    --config.file=/etc/prometheus/prometheus.yml \
    --storage.tsdb.path=/prometheus \
    --storage.tsdb.retention.time=15d \
    --web.console.libraries=/usr/share/prometheus/console_libraries \
    --web.console.templates=/usr/share/prometheus/consoles" \
  && echo "  prometheus: deployed" || echo "  prometheus: failed"
fi
step_done

# ── Ensure PostgreSQL HA cluster ──────────────────────────────────────
step_start "Ensuring PostgreSQL HA cluster (repmgr)"

# Check if HA cluster already exists (check for hive-pg-0)
# shellcheck disable=SC2029
HA_EXISTS=$(ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
  "docker service inspect hive-pg-0 >/dev/null 2>&1 && echo yes || echo no")

if [ "$HA_EXISTS" = "yes" ]; then
  echo "  PostgreSQL HA cluster already running"
else
  # Check for old single-node postgres
  # shellcheck disable=SC2029
  OLD_PG_EXISTS=$(ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
    "docker service inspect hive-postgres >/dev/null 2>&1 && echo yes || echo no")
  if [ "$OLD_PG_EXISTS" = "yes" ]; then
    echo "  WARNING: Old hive-postgres service detected."
    echo "  The HA cluster will be created alongside it."
    echo "  After verifying HA works, remove the old service:"
    echo "    docker service rm hive-postgres"
  fi

  # Get manager node hostnames (only ready+active nodes, sorted)
  # shellcheck disable=SC2029
  MANAGER_HOSTS=$(ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
    "docker node ls --filter role=manager --format '{{.Hostname}} {{.Status}}' | grep ' Ready' | awk '{print \$1}' | sort")

  if [ -z "$MANAGER_HOSTS" ]; then
    echo "  ERROR: No manager nodes found. Cannot deploy HA postgres."
  else
    PG_PASSWORD=$(openssl rand -hex 24)
    REPMGR_PASSWORD=$(openssl rand -hex 24)
    PG_ADMIN_PASSWORD=$(openssl rand -hex 24)

    # Build partner node list using numeric indices
    PARTNER_NODES=""
    NODE_IDX=0
    for host in $MANAGER_HOSTS; do
      SVC_NAME="hive-pg-${NODE_IDX}"
      if [ -n "$PARTNER_NODES" ]; then
        PARTNER_NODES="${PARTNER_NODES},"
      fi
      PARTNER_NODES="${PARTNER_NODES}${SVC_NAME}"
      NODE_IDX=$((NODE_IDX + 1))
    done

    PRIMARY_HOST="hive-pg-0"

    # Create per-node PostgreSQL services
    NODE_IDX=0
    for host in $MANAGER_HOSTS; do
      SVC_NAME="hive-pg-${NODE_IDX}"
      REPMGR_ID=$((1000 + NODE_IDX))
      echo "  ${SVC_NAME}: deploying on ${host}..."
      # shellcheck disable=SC2029
      ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" "docker service create \
        --name ${SVC_NAME} \
        --network ${HIVE_NETWORK:-hive-net} \
        --label hive.managed=true \
        --label hive.role=postgres-ha \
        --label hive.pg.hostname=${host} \
        --constraint node.role==manager \
        --constraint node.hostname==${host} \
        --mount type=volume,src=hive-pg-${NODE_IDX}-data,dst=/bitnami/postgresql \
        --restart-condition on-failure \
        --restart-max-attempts 10 \
        -e POSTGRESQL_POSTGRES_PASSWORD=${PG_ADMIN_PASSWORD} \
        -e POSTGRESQL_USERNAME=hive \
        -e POSTGRESQL_PASSWORD=${PG_PASSWORD} \
        -e POSTGRESQL_DATABASE=hive \
        -e REPMGR_PASSWORD=${REPMGR_PASSWORD} \
        -e REPMGR_PRIMARY_HOST=${PRIMARY_HOST} \
        -e REPMGR_PARTNER_NODES=${PARTNER_NODES} \
        -e REPMGR_NODE_NAME=${SVC_NAME} \
        -e REPMGR_NODE_NETWORK_NAME=${SVC_NAME} \
        -e REPMGR_NODE_ID=${REPMGR_ID} \
        -e POSTGRESQL_NUM_SYNCHRONOUS_REPLICAS=1 \
        bitnamilegacy/postgresql-repmgr:latest" \
      && echo "    ${SVC_NAME}: ok" || echo "    ${SVC_NAME}: FAILED"
      NODE_IDX=$((NODE_IDX + 1))
    done

    # Store PG password as a Swarm secret for future use
    # shellcheck disable=SC2029
    ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
      "docker secret inspect hive-db-password >/dev/null 2>&1 || \
       echo '${PG_PASSWORD}' | docker secret create hive-db-password -" \
    && echo "  hive-db-password: created" || echo "  hive-db-password: already exists or FAILED"

    # Build multi-host DATABASE_URL for failover (lib/pq tries hosts in order)
    PG_HOSTS=""
    for idx in $(seq 0 $((NODE_IDX - 1))); do
      if [ -n "$PG_HOSTS" ]; then
        PG_HOSTS="${PG_HOSTS},"
      fi
      PG_HOSTS="${PG_HOSTS}hive-pg-${idx}"
    done
    NEW_DB_URL="postgres://hive:${PG_PASSWORD}@${PG_HOSTS}:5432/hive?sslmode=disable"
    echo "  Updating DATABASE_URL on hive-manager and hive-engine (hosts: ${PG_HOSTS})..."
    # shellcheck disable=SC2029
    ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
      "docker service update --env-add DATABASE_URL='${NEW_DB_URL}' hive-manager 2>/dev/null || true"
    # shellcheck disable=SC2029
    ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
      "docker service update --env-add DATABASE_URL='${NEW_DB_URL}' hive-engine 2>/dev/null || true"
  fi
fi
step_done

# ── Ensure Docker Swarm secrets ───────────────────────────────────────
step_start "Ensuring Docker Swarm secrets"

for SECRET_NAME in hive-auth-secret hive-engine-secret; do
  # shellcheck disable=SC2029
  SECRET_EXISTS=$(ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
    "docker secret inspect ${SECRET_NAME} >/dev/null 2>&1 && echo yes || echo no")
  if [ "$SECRET_EXISTS" = "no" ]; then
    SECRET_VAL=$(openssl rand -hex 32)
    # shellcheck disable=SC2029
    ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
      "echo '${SECRET_VAL}' | docker secret create ${SECRET_NAME} -" \
    && echo "  ${SECRET_NAME}: created" || echo "  ${SECRET_NAME}: FAILED"
  else
    echo "  ${SECRET_NAME}: exists"
  fi
done

# Ensure secrets are attached to services that need them
for SVC in hive-manager hive-engine; do
  # shellcheck disable=SC2029
  SVC_EXISTS=$(ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
    "docker service inspect ${SVC} >/dev/null 2>&1 && echo yes || echo no")
  if [ "$SVC_EXISTS" = "yes" ]; then
    # Check which secrets are already attached
    # shellcheck disable=SC2029
    ATTACHED_SECRETS=$(ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
      "docker service inspect ${SVC} --format '{{range .Spec.TaskTemplate.ContainerSpec.Secrets}}{{.SecretName}} {{end}}'" 2>/dev/null || true)

    SECRET_ADD_FLAGS=""
    if [ "$SVC" = "hive-manager" ]; then
      NEEDED_SECRETS="hive-db-password hive-engine-secret hive-auth-secret"
    else
      NEEDED_SECRETS="hive-db-password hive-engine-secret"
    fi

    for NEEDED in $NEEDED_SECRETS; do
      if ! echo "$ATTACHED_SECRETS" | grep -q "$NEEDED"; then
        SECRET_ADD_FLAGS="${SECRET_ADD_FLAGS} --secret-add ${NEEDED}"
      fi
    done

    if [ -n "$SECRET_ADD_FLAGS" ]; then
      echo "  ${SVC}: attaching secrets${SECRET_ADD_FLAGS}"
      # shellcheck disable=SC2029,SC2086
      ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
        "docker service update ${SECRET_ADD_FLAGS} ${SVC}" 2>/dev/null || \
        echo "  WARNING: could not attach secrets to ${SVC}"
    else
      echo "  ${SVC}: all secrets attached"
    fi
  fi
done
step_done

# ── Update swarm services ─────────────────────────────────────────────
step_start "Pushing image and updating services"
# Push already happened via docker buildx --push

for svc in "${UPDATE_SERVICES[@]}"; do
  echo "  -> updating ${svc}"
  if [ "$svc" = "hive-manager" ] && [ -n "$HIVE_URL" ]; then
    # shellcheck disable=SC2029
    ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" "docker service update --force \
      --image ${FULL_IMAGE} \
      --label-add traefik.enable=true \
      --label-add 'traefik.http.routers.hive.rule=PathPrefix(\`/\`)' \
      --label-add traefik.http.routers.hive.entrypoints=web,websecure \
      --label-add traefik.http.services.hive.loadbalancer.server.port=8080 \
      --env-add HIVE_URL=${HIVE_URL} \
      --env-add ORIGIN=${HIVE_URL} \
      --env-rm HIVE_ENGINE_SECRET --env-rm BETTER_AUTH_SECRET \
      ${svc}" || \
      echo "     WARNING: ${svc} update failed"
  elif [ "$svc" = "hive-agent" ]; then
    # shellcheck disable=SC2029
    ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
      "docker service update --force --image ${FULL_IMAGE} \
      --env-add 'NODE_HOSTNAME={{.Node.Hostname}}' \
      --mount-add type=bind,source=/,target=/rootfs,readonly \
      --cap-add SYS_ADMIN \
      ${svc}" || \
      echo "     WARNING: ${svc} not found or update failed"
  elif [ "$svc" = "hive-engine" ]; then
    # shellcheck disable=SC2029
    ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
      "docker service update --force --image ${FULL_IMAGE} \
      --env-rm HIVE_ENGINE_SECRET \
      --label-add traefik.enable=true \
      --label-add 'traefik.http.routers.hive-api.rule=PathPrefix(\`/api/v1/\`)' \
      --label-add traefik.http.routers.hive-api.entrypoints=web,websecure \
      --label-add traefik.http.routers.hive-api.priority=20 \
      --label-add traefik.http.services.hive-api.loadbalancer.server.port=9090 \
      --label-add 'traefik.http.routers.hive-ws.rule=PathPrefix(\`/ws/\`)' \
      --label-add traefik.http.routers.hive-ws.entrypoints=web,websecure \
      --label-add traefik.http.routers.hive-ws.priority=20 \
      --label-add traefik.http.routers.hive-ws.service=hive-api \
      ${svc}" || \
      echo "     WARNING: ${svc} not found or update failed"
  else
    # shellcheck disable=SC2029
    ssh -o StrictHostKeyChecking=no "${SSH_TARGET}" \
      "docker service update --force --image ${FULL_IMAGE} ${svc}" || \
      echo "     WARNING: ${svc} not found or update failed"
  fi
done
step_done

# ── Summary ────────────────────────────────────────────────────────────
TOTAL_ELAPSED=$(( $(date +%s) - TOTAL_START ))
echo ""
echo "━━━ Deploy complete (${TOTAL_ELAPSED}s total) ━━━"
echo "  Image:    ${FULL_IMAGE}"
echo "  Services: ${UPDATE_SERVICES[*]}"
echo ""
echo "Useful commands:"
echo "  make remote-logs          # tail manager logs"
echo "  make remote-logs-engine   # tail engine logs"
echo "  make remote-status        # service overview"
