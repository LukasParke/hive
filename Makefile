.PHONY: build run dev test clean docker ui-dev ui-build

BINARY=hive
GO_FILES=$(shell find . -name '*.go' -not -path './ui/*')

build:
	go build -o bin/$(BINARY) ./cmd/hive

run: build
	HIVE_DEV=1 HIVE_UI_DIR="" ./bin/$(BINARY)

dev:
	@echo "Starting Go API in dev mode..."
	HIVE_DEV=1 HIVE_UI_DIR="" go run ./cmd/hive &
	@echo "Start UI separately with: make ui-dev"

test:
	go test -race -cover ./...

test-coverage:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

clean:
	rm -rf bin/
	rm -rf ui/build ui/node_modules

docker:
	docker build -t hive:latest .

docker-run:
	docker run -d \
		--name hive \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v hive-data:/data \
		-p 80:80 \
		-p 443:443 \
		-p 8080:8080 \
		hive:latest

ui-dev:
	cd ui && npm run dev

ui-build:
	cd ui && npm run build

lint:
	golangci-lint run ./...

migrate-up:
	migrate -path internal/store/migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path internal/store/migrations -database "$(DATABASE_URL)" down 1
