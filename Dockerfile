### Stage 1: Build SvelteKit frontend
FROM node:22-slim AS ui-builder
WORKDIR /app/ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui/ .
RUN npm run build

### Stage 2: Build Go binary
FROM golang:1.25-bookworm AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /hive ./cmd/hive

### Stage 3: Runtime (Go + Node.js for SvelteKit/BetterAuth)
FROM node:22-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends git ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*

COPY --from=go-builder /hive /usr/local/bin/hive
COPY --from=ui-builder /app/ui/build /app/ui
COPY templates/ /app/templates/
COPY internal/store/migrations/ /app/migrations/

WORKDIR /app

ENV HIVE_ROLE=manager
ENV HIVE_DATA_DIR=/data
ENV HIVE_UI_DIR=/app/ui
ENV HIVE_API_PORT=8080

VOLUME /data
EXPOSE 80 443 8080

ENTRYPOINT ["hive"]
