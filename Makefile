.PHONY: build run dev test clean docker docker-clean ui-dev ui-build logs logs-postgres status

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

docker-clean:
	docker build --no-cache -t hive:latest .

deploy: docker
	docker tag hive:latest 127.0.0.1:5000/hive:latest
	docker push 127.0.0.1:5000/hive:latest
	docker service update --force --image 127.0.0.1:5000/hive:latest hive-manager
	docker service update --force --image 127.0.0.1:5000/hive:latest hive-agent

deploy-clean: docker-clean
	docker tag hive:latest 127.0.0.1:5000/hive:latest
	docker push 127.0.0.1:5000/hive:latest
	docker service update --force --image 127.0.0.1:5000/hive:latest hive-manager
	docker service update --force --image 127.0.0.1:5000/hive:latest hive-agent

docker-run:
	docker run -d \
		--name hive \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v hive-data:/data \
		-p 80:80 \
		-p 443:443 \
		-p 8080:8080 \
		hive:latest

# Frontend dev targets (requires Node.js 22+, not needed for production)
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

# Troubleshooting targets (require Docker Swarm)
logs:
	docker service logs -f --tail 100 hive-manager

logs-postgres:
	docker service logs -f --tail 100 hive-postgres

status:
	@echo "=== Hive Services ==="
	docker service ls --filter label=hive.managed=true
	@echo ""
	@echo "=== hive-manager Tasks ==="
	docker service ps hive-manager --no-trunc
