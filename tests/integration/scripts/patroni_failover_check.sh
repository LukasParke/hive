#!/usr/bin/env bash
# Focused Patroni failover check for the weekly HA integration workflow.
#
# Waits until the patroni cluster has elected a primary, kills the primary's
# container, asserts a NEW primary is elected within the timeout, and that
# the control-plane API answers again afterwards. Exits non-zero (leaving
# debug output in the workflow log) on any failure.
set -euo pipefail

MANAGER_HOST="${DIND_MANAGER_HOST:-tcp://127.0.0.1:2375}"
PATRONI_SERVICE="${PATRONI_SERVICE:-patroni_patroni}"
FAILOVER_TIMEOUT="${PATRONI_FAILOVER_TIMEOUT_SECONDS:-60}"
API_URL="${HIVE_API_BASE_URL:-http://127.0.0.1:3000/api/v1/health}"

log() { echo "[patroni-check] $*"; }
fail() { echo "[patroni-check] FAIL: $*" >&2; exit 1; }

patroni_containers() {
  docker --host "$MANAGER_HOST" ps \
    --filter "label=com.docker.swarm.service.name=$PATRONI_SERVICE" \
    --format '{{.Names}}'
}

patronictl_list() {
  local c="$1" cfg
  for cfg in /etc/patroni/patroni.yml /etc/patroni.yml /patroni.yml; do
    if docker --host "$MANAGER_HOST" exec "$c" test -f "$cfg" >/dev/null 2>&1; then
      if docker --host "$MANAGER_HOST" exec "$c" patronictl -c "$cfg" list 2>/dev/null; then
        return 0
      fi
    fi
  done
  docker --host "$MANAGER_HOST" exec "$c" patronictl list 2>/dev/null
}

# Extract the Host column of the first Leader/Primary row of a
# `patronictl list` table (columns: Cluster | Member | Host | Role | State).
primary_host_from() {
  awk -F'|' '/\|/ {
    for (i = 1; i <= NF; i++) gsub(/^[ \t]+|[ \t]+$/, "", $i)
    role = tolower($0)
    if (role ~ /leader/ || role ~ /primary/) { print $3; exit }
  }'
}

wait_for_primary() {
  local deadline=$((SECONDS + FAILOVER_TIMEOUT)) c out host
  while [ "$SECONDS" -lt "$deadline" ]; do
    for c in $(patroni_containers); do
      out="$(patronictl_list "$c" || true)"
      host="$(printf '%s' "$out" | primary_host_from || true)"
      if [ -n "$host" ]; then
        printf '%s %s\n' "$c" "$host"
        return 0
      fi
    done
    sleep 3
  done
  return 1
}

log "waiting up to ${FAILOVER_TIMEOUT}s for a healthy patroni cluster with an elected primary"
read -r PRIMARY_CONTAINER PRIMARY_HOST < <(wait_for_primary) ||
  fail "patroni cluster never elected a primary; service=$PATRONI_SERVICE"
log "primary is $PRIMARY_HOST (container $PRIMARY_CONTAINER)"

log "killing primary container $PRIMARY_CONTAINER"
docker --host "$MANAGER_HOST" stop -t 10 "$PRIMARY_CONTAINER" >/dev/null

NEW_PRIMARY=""
deadline=$((SECONDS + FAILOVER_TIMEOUT))
while [ "$SECONDS" -lt "$deadline" ]; do
  for c in $(patroni_containers); do
    [ "$c" = "$PRIMARY_CONTAINER" ] && continue
    out="$(patronictl_list "$c" || true)"
    host="$(printf '%s' "$out" | primary_host_from || true)"
    if [ -n "$host" ] && [ "$host" != "$PRIMARY_HOST" ]; then
      NEW_PRIMARY="$host"
      break 2
    fi
  done
  sleep 2
done
[ -n "$NEW_PRIMARY" ] || fail "no new primary elected within ${FAILOVER_TIMEOUT}s"
log "new primary elected: $NEW_PRIMARY (failover took <= ${SECONDS}s)"

deadline=$((SECONDS + FAILOVER_TIMEOUT))
until curl -fsS "$API_URL" >/dev/null 2>&1; do
  [ "$SECONDS" -lt "$deadline" ] || fail "API $API_URL did not recover after failover"
  sleep 2
done
log "control-plane API healthy after failover"
log "PASS"
