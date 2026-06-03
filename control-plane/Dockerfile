FROM node:22-alpine AS ui-builder
WORKDIR /ui
COPY ui/package.json ui/package-lock.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

FROM golang:1.25-alpine AS go-builder
WORKDIR /src
COPY . .
ARG VERSION=dev
RUN go mod download && go build -ldflags="-s -w -X github.com/luke/hive/control-plane/internal/version.Current=${VERSION}" -o /out/control-plane ./control-plane/cmd/control-plane

FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget
COPY --from=go-builder /out/control-plane /control-plane
COPY --from=ui-builder /ui/dist /ui/dist
COPY control-plane/internal/db/migrations /internal/db/migrations
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:3000/api/v1/health || exit 1
ENTRYPOINT ["/control-plane"]
