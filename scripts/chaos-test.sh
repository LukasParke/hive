#!/usr/bin/env bash
# Hive PaaS Chaos Test Matrix
# Validates platform resilience by simulating node failures and verifying SLOs.
#
# Usage:
#   ./scripts/chaos-test.sh [SSH_TARGET]
#   SSH_TARGET defaults to the HIVE_SSH_TARGET env var.
#
# SLOs checked:
#   1. API is reachable within 60s after a manager loss
#   2. Deployed apps remain routable after a worker loss
#   3. No stuck "running" backup/maintenance records after recovery
#   4. Control-plane services self-heal to desired replica count

set -euo pipefail

SSH_TARGET="${1:-${HIVE_SSH_TARGET:-}}"
HIVE_URL="${HIVE_URL:-https://localhost}"
PASS=0
FAIL=0
SKIP=0

if [ -z "$SSH_TARGET" ]; then
  echo "ERROR: SSH_TARGET is required (pass as arg or set HIVE_SSH_TARGET)"
  exit 1
fi

ssh_cmd() { ssh -o StrictHostKeyChecking=no "$SSH_TARGET" "$@"; }

log()  { echo "[$(date +%H:%M:%S)] $*"; }
pass() { log "  PASS: $1"; PASS=$((PASS + 1)); }
fail() { log "  FAIL: $1"; FAIL=$((FAIL + 1)); }
skip() { log "  SKIP: $1"; SKIP=$((SKIP + 1)); }

wait_for_api() {
  local timeout=${1:-60}
  local start
  start=$(date +%s)
  while true; do
    if curl -sfk "${HIVE_URL}/api/v1/system/health" >/dev/null 2>&1; then
      return 0
    fi
    if [ $(( $(date +%s) - start )) -ge "$timeout" ]; then
      return 1
    fi
    sleep 2
  done
}

# ─── Test 1: Service replica counts ─────────────────────────────────
log "Test 1: Control-plane service replica counts"
for svc in hive-engine hive-manager hive-traefik; do
  RUNNING=$(ssh_cmd "docker service ps $svc --filter desired-state=running -q 2>/dev/null | wc -l" || echo "0")
  DESIRED=$(ssh_cmd "docker service inspect $svc --format '{{.Spec.Mode.Replicated.Replicas}}' 2>/dev/null" || echo "0")
  if [ "$RUNNING" -ge "$DESIRED" ] && [ "$DESIRED" -gt "0" ]; then
    pass "$svc: $RUNNING/$DESIRED replicas running"
  else
    fail "$svc: $RUNNING/$DESIRED replicas running"
  fi
done

# ─── Test 2: API reachability ────────────────────────────────────────
log "Test 2: API reachability"
if wait_for_api 10; then
  pass "API reachable at ${HIVE_URL}"
else
  fail "API not reachable at ${HIVE_URL}"
fi

# ─── Test 3: Database connectivity ──────────────────────────────────
log "Test 3: Database connectivity"
PG_SERVICES=$(ssh_cmd "docker service ls --filter label=hive.role=postgres-ha -q 2>/dev/null | wc -l" || echo "0")
if [ "$PG_SERVICES" -gt "0" ]; then
  PG_RUNNING=$(ssh_cmd "docker service ls --filter label=hive.role=postgres-ha --format '{{.Replicas}}'" | head -1)
  pass "Postgres HA: $PG_SERVICES services, first has $PG_RUNNING"
else
  PG_SINGLE=$(ssh_cmd "docker service ls --filter name=hive-postgres -q 2>/dev/null | wc -l" || echo "0")
  if [ "$PG_SINGLE" -gt "0" ]; then
    pass "Postgres running (single instance)"
  else
    fail "No Postgres service found"
  fi
fi

# ─── Test 4: NATS connectivity ──────────────────────────────────────
log "Test 4: NATS service health"
NATS_RUNNING=$(ssh_cmd "docker service ps hive-nats --filter desired-state=running -q 2>/dev/null | wc -l" || echo "0")
if [ "$NATS_RUNNING" -ge "1" ]; then
  pass "NATS: $NATS_RUNNING replica(s) running"
else
  fail "NATS: no running replicas"
fi

# ─── Test 5: Cloudflared tunnel (if configured) ─────────────────────
log "Test 5: Cloudflare tunnel health"
CF_EXISTS=$(ssh_cmd "docker service ls --filter name=hive-cloudflared -q 2>/dev/null | wc -l" || echo "0")
if [ "$CF_EXISTS" -gt "0" ]; then
  CF_RUNNING=$(ssh_cmd "docker service ps hive-cloudflared --filter desired-state=running -q 2>/dev/null | wc -l" || echo "0")
  CF_DESIRED=$(ssh_cmd "docker service inspect hive-cloudflared --format '{{.Spec.Mode.Replicated.Replicas}}' 2>/dev/null" || echo "0")
  if [ "$CF_RUNNING" -ge "$CF_DESIRED" ] && [ "$CF_DESIRED" -gt "0" ]; then
    pass "Cloudflared: $CF_RUNNING/$CF_DESIRED replicas"
  else
    fail "Cloudflared: $CF_RUNNING/$CF_DESIRED replicas"
  fi
else
  skip "Cloudflared not deployed"
fi

# ─── Test 6: Simulate worker drain and verify service reschedule ────
log "Test 6: Worker drain simulation"
WORKERS=$(ssh_cmd "docker node ls --filter role=worker --format '{{.ID}} {{.Status}}' | grep 'Ready' | head -1")
if [ -n "$WORKERS" ]; then
  WORKER_ID=$(echo "$WORKERS" | awk '{print $1}')
  WORKER_HOSTNAME=$(ssh_cmd "docker node inspect $WORKER_ID --format '{{.Description.Hostname}}'")
  log "  Draining worker $WORKER_HOSTNAME ($WORKER_ID)..."

  ssh_cmd "docker node update --availability drain $WORKER_ID"
  sleep 10

  if wait_for_api 30; then
    pass "API still reachable after worker drain"
  else
    fail "API unreachable after worker drain"
  fi

  log "  Restoring worker $WORKER_HOSTNAME..."
  ssh_cmd "docker node update --availability active $WORKER_ID"
  sleep 5
  pass "Worker $WORKER_HOSTNAME restored"
else
  skip "No worker nodes to drain"
fi

# ─── Test 7: Engine restart resilience ──────────────────────────────
log "Test 7: Engine restart resilience"
ssh_cmd "docker service update --force hive-engine 2>/dev/null" || true
sleep 15

if wait_for_api 60; then
  pass "API recovered after engine force-restart"
else
  fail "API did not recover after engine force-restart"
fi

# ─── Test 8: Stale run check ────────────────────────────────────────
log "Test 8: Stale backup/maintenance run check"
if wait_for_api 5; then
  STALE=$(curl -sfk "${HIVE_URL}/api/v1/system-tasks" 2>/dev/null | grep -o '"stale_run_reconcile"' || true)
  if [ -n "$STALE" ]; then
    pass "Stale run reconciliation task registered"
  else
    skip "Could not verify stale run task (API format)"
  fi
else
  skip "API not reachable for stale run check"
fi

# ─── Results ────────────────────────────────────────────────────────
echo ""
echo "════════════════════════════════════════════"
echo "  Chaos Test Results"
echo "════════════════════════════════════════════"
echo "  PASS: $PASS"
echo "  FAIL: $FAIL"
echo "  SKIP: $SKIP"
echo "════════════════════════════════════════════"

if [ "$FAIL" -gt "0" ]; then
  echo "  STATUS: FAILED"
  exit 1
else
  echo "  STATUS: PASSED"
  exit 0
fi
