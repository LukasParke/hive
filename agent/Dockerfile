FROM golang:1.25-alpine AS builder
WORKDIR /src

# Build outside the Go workspace: only the agent and proto modules are copied
# into this image, and agent/go.mod has a local replace for proto.
ENV GOWORK=off

# Copy module files
COPY proto/go.mod proto/go.sum ./proto/
COPY agent/go.mod agent/go.sum ./agent/

# Download dependencies
RUN cd agent && go mod download

# Copy source
COPY proto/ ./proto/
COPY agent/ ./agent/

# Build
ARG VERSION=dev
RUN cd agent && CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=${VERSION}" -o /out/agent ./cmd/agent

FROM alpine:3.21
RUN apk add --no-cache ca-certificates util-linux
COPY --from=builder /out/agent /agent
EXPOSE 9090 9091
ENTRYPOINT ["/agent"]
