.PHONY: test build ui images lint proto

VERSION ?= dev
REGISTRY ?= ghcr.io/lukasparke/hive

 test:
	cd control-plane && go test ./...
	cd agent && go test ./...

build:
	cd control-plane && go build -ldflags="-X github.com/luke/hive/control-plane/internal/version.Current=$(VERSION)" ./cmd/control-plane
	cd agent && go build -ldflags="-X main.Version=$(VERSION)" ./cmd/agent

ui:
	cd ui && npm install && npm run build

images:
	docker build -t $(REGISTRY)/control-plane:$(VERSION) -f control-plane/Dockerfile .
	docker build -t $(REGISTRY)/agent:$(VERSION) -f agent/Dockerfile .

lint:
	cd control-plane && go vet ./...
	cd agent && go vet ./...
	cd control-plane && test -x $$(which golangci-lint) && golangci-lint run ./... || true
	cd agent && test -x $$(which golangci-lint) && golangci-lint run ./... || true

proto:
	cd proto && buf generate
