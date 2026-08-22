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
create_secret "hive-jwt-secret" "ci-jwt-secret-must-be-at-least-32-characters-long"
create_secret "agent-bootstrap-token" "ci-bootstrap-token"

# The stack mounts the hive-agent-ca config into the agent, so it must exist
# before the first `docker stack deploy`. Seed a throwaway self-signed CA;
# the control-plane replaces it with its real CA at boot (best-effort).
if ! docker --host "$MANAGER_HOST" config ls --format '{{.Name}}' | grep -q '^hive-agent-ca$'; then
  if command -v openssl >/dev/null 2>&1; then
    openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
      -keyout /dev/null -nodes -subj '/CN=hive-internal-ci-seed' \
      -days 1 2>/dev/null | docker --host "$MANAGER_HOST" config create "hive-agent-ca" -
  fi
fi

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

# BuildKit cannot run as a swarm service (services lack CAP_SYS_ADMIN and
# buildkitd's snapshotter needs mount(2)). Run it as a privileged
# host-level container aliased as 'buildkit' on hive_internal; the control
# plane reaches it via BUILDKIT_ADDR=tcp://buildkit:1234.
if ! docker --host "$MANAGER_HOST" ps --format '{{.Names}}' | grep -qx 'hive-buildkit'; then
  # buildkitd.toml: allow the plain-HTTP internal registry.
  mkdir -p /etc/hive
  printf '[registry."registry:5000"]\n  http = true\n  insecure = true\n' > /etc/hive/buildkitd.toml
docker --host "$MANAGER_HOST" rm -f hive-buildkit >/dev/null 2>&1 || true
  docker --host "$MANAGER_HOST" run -d --privileged --restart=unless-stopped \
    --name hive-buildkit \
    --network hive_internal --network-alias buildkit-ci \
    -v /etc/hive/buildkitd.toml:/etc/buildkit/buildkitd.toml:ro \
    -v hive_buildkit-cache:/var/lib/buildkit \
    moby/buildkit:latest --addr tcp://0.0.0.0:1234 >/dev/null
fi
