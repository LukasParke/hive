### Stage 1: Build SvelteKit app (adapter-node) + launcher
FROM node:22-slim AS ui-builder
RUN apt-get update && apt-get install -y --no-install-recommends openssl && rm -rf /var/lib/apt/lists/*
WORKDIR /app/ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui/ .
RUN npx svelte-kit sync
RUN npx prisma generate
RUN BETTER_AUTH_SECRET=build-placeholder npm run build
RUN npm run build:launcher

### Stage 2: Build Go binaries (hive + hive-engine)
FROM golang:1.25-bookworm AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /hive ./cmd/hive && \
    CGO_ENABLED=0 GOOS=linux go build -o /hive-engine ./cmd/engine

### Stage 3: Runtime (Node.js + Go binary)
FROM node:22-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends git ca-certificates curl procps && \
    rm -rf /var/lib/apt/lists/*

COPY --from=go-builder /hive /usr/local/bin/hive
COPY --from=go-builder /hive-engine /usr/local/bin/hive-engine

WORKDIR /app

COPY --from=ui-builder /app/ui/build ./build
COPY --from=ui-builder /app/ui/dist/launcher.js ./scripts/launcher.js
COPY --from=ui-builder /app/ui/package.json ./package.json
COPY --from=ui-builder /app/ui/node_modules ./node_modules
COPY --from=ui-builder /app/ui/prisma ./prisma
COPY --from=ui-builder /app/ui/prisma.config.ts ./prisma.config.ts
COPY --from=ui-builder /app/ui/scripts/server.js ./server.js

COPY templates/ /app/templates/

ENV HIVE_ROLE=manager
ENV HIVE_DATA_DIR=/data
ENV NODE_ENV=production
ENV PORT=8080

VOLUME /data
EXPOSE 80 443 8080

COPY ui/scripts/entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
  CMD if [ "$HIVE_ROLE" = "agent" ]; then pgrep -x hive >/dev/null; \
      elif [ "$HIVE_ROLE" = "engine" ]; then curl -sf http://localhost:9090/engine/v1/healthz; \
      else curl -sf http://localhost:8080/healthz; fi

ENTRYPOINT ["/app/entrypoint.sh"]
