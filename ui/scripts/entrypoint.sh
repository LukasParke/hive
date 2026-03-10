#!/bin/sh
set -e

# Read Docker Swarm secrets from files if available.
# Only construct DATABASE_URL from secret if it's not already set as an env var.
if [ -z "$DATABASE_URL" ]; then
  if [ -f /run/secrets/hive-db-password ]; then
    DB_PASS=$(cat /run/secrets/hive-db-password)
    export DATABASE_URL="postgres://hive:${DB_PASS}@hive-pg-0:5432/hive?sslmode=disable"
  elif [ -f /run/secrets/hive-pg-password ]; then
    DB_PASS=$(cat /run/secrets/hive-pg-password)
    export DATABASE_URL="postgres://hive:${DB_PASS}@hive-pg-0:5432/hive?sslmode=disable"
  fi
fi

if [ -f /run/secrets/hive-engine-secret ]; then
  export HIVE_ENGINE_SECRET=$(cat /run/secrets/hive-engine-secret)
fi

if [ -f /run/secrets/hive-auth-secret ]; then
  export BETTER_AUTH_SECRET=$(cat /run/secrets/hive-auth-secret)
fi

if [ -n "$HIVE_URL" ] && [ -z "$ORIGIN" ]; then
  export ORIGIN="$HIVE_URL"
fi

ROLE="${HIVE_ROLE:-manager}"

echo "[hive] Role: $ROLE"
echo "[hive] ORIGIN: ${ORIGIN:-<not set>}"
echo "[hive] DATABASE_URL set: $([ -n "$DATABASE_URL" ] && echo 'yes' || echo 'no')"
echo "[hive] HIVE_ENGINE_SECRET set: $([ -n "$HIVE_ENGINE_SECRET" ] && echo 'yes' || echo 'no')"
echo "[hive] BETTER_AUTH_SECRET set: $([ -n "$BETTER_AUTH_SECRET" ] && echo 'yes' || echo 'no')"

case "$ROLE" in
  manager)
    if [ "$HIVE_MANAGED" = "true" ]; then
      echo "[hive] Starting as managed manager (SvelteKit server)"
      if [ -f /app/prisma/schema.prisma ] && [ -n "$DATABASE_URL" ]; then
        echo "[hive] Running Prisma migrations..."
        cd /app
        MIGRATE_OUTPUT=$(npx prisma migrate deploy 2>&1) || true
        echo "$MIGRATE_OUTPUT"

        if echo "$MIGRATE_OUTPUT" | grep -q "P3005"; then
          echo "[hive] Detected P3005 (non-empty schema). Baselining existing migrations..."
          for dir in prisma/migrations/*/; do
            migration_name=$(basename "$dir")
            if [ "$migration_name" != "migration_lock.toml" ]; then
              echo "[hive] Executing SQL for $migration_name..."
              npx prisma db execute --file "$dir/migration.sql" 2>&1 || true
              echo "[hive] Resolving $migration_name as applied..."
              npx prisma migrate resolve --applied "$migration_name" 2>&1 || true
            fi
          done
          echo "[hive] Baseline complete. Running migrate deploy for any new migrations..."
          if ! npx prisma migrate deploy 2>&1; then
            echo "[hive] FATAL: Prisma migrations failed after baseline. Exiting."
            exit 1
          fi
        elif echo "$MIGRATE_OUTPUT" | grep -qi "error"; then
          echo "[hive] ERROR: Prisma migrate deploy failed."
          echo "[hive] Will retry once after 5 seconds..."
          sleep 5
          if ! npx prisma migrate deploy 2>&1; then
            echo "[hive] FATAL: Prisma migrations failed after retry. Exiting."
            exit 1
          fi
        fi
        echo "[hive] Migrations applied successfully."
      fi
      exec node /app/server.js
    else
      echo "[hive] Starting launcher (bootstrap mode)"
      exec node /app/scripts/launcher.js
    fi
    ;;
  engine)
    echo "[hive] Starting as engine only"
    exec hive-engine
    ;;
  agent)
    echo "[hive] Starting as agent"
    exec hive
    ;;
  *)
    echo "[hive] Unknown role: $ROLE"
    exit 1
    ;;
esac
