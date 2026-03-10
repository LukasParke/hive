.PHONY: build run dev test clean docker docker-clean ui-dev ui-build logs logs-postgres status \
       remote-deploy remote-deploy-clean remote-logs remote-logs-current remote-logs-engine \
       remote-logs-agent remote-logs-postgres remote-logs-all remote-status remote-shell \
       remote-restart remote-ps remote-cleanup

GO_FILES=$(shell find . -name '*.go' -not -path './ui/*')

# Load remote deploy config (optional — won't error if missing)
-include deploy.env
export REMOTE_HOST REMOTE_USER REMOTE_DIR REGISTRY IMAGE

build:
	go build -o bin/hive ./cmd/hive
	go build -o bin/hive-engine ./cmd/engine

run-engine: build
	HIVE_DEV=1 ./bin/hive-engine

run-agent: build
	HIVE_DEV=1 ./bin/hive agent

dev:
	@echo "Starting hive-engine in dev mode..."
	HIVE_DEV=1 go run ./cmd/engine &
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
	docker service update --force --image 127.0.0.1:5000/hive:latest hive-engine
	docker service update --force --image 127.0.0.1:5000/hive:latest hive-agent

deploy-clean: docker-clean
	docker tag hive:latest 127.0.0.1:5000/hive:latest
	docker push 127.0.0.1:5000/hive:latest
	docker service update --force --image 127.0.0.1:5000/hive:latest hive-manager
	docker service update --force --image 127.0.0.1:5000/hive:latest hive-engine
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

# Frontend dev targets
ui-dev:
	cd ui && npm run dev

ui-build:
	cd ui && BETTER_AUTH_SECRET=build npm run build

lint:
	golangci-lint run ./...

# Troubleshooting targets (require Docker Swarm)
logs:
	docker service logs -f --tail 100 hive-manager

logs-engine:
	docker service logs -f --tail 100 hive-engine

logs-postgres:
	docker service logs -f --tail 100 hive-postgres

status:
	@echo "=== Hive Services ==="
	docker service ls --filter label=hive.managed=true
	@echo ""
	@echo "=== Service Tasks ==="
	docker service ps hive-manager --no-trunc 2>/dev/null || true
	docker service ps hive-engine --no-trunc 2>/dev/null || true

# ── Remote deployment targets (use deploy.env for config) ─────────────
REMOTE_SSH = ssh -o StrictHostKeyChecking=no $(REMOTE_USER)@$(REMOTE_HOST)

remote-deploy:
	@bash scripts/remote-deploy.sh

remote-deploy-clean:
	@bash scripts/remote-deploy.sh --no-cache

remote-restart:
	@bash scripts/remote-deploy.sh --restart-only

remote-logs:
	$(REMOTE_SSH) docker service logs -f --tail 200 hive-manager

remote-logs-current:
	@$(REMOTE_SSH) 'TASK=$$(docker service ps hive-manager -f desired-state=running --format "{{.ID}}" | head -1); \
	docker service logs -f --tail 200 hive-manager 2>&1 | grep "$$TASK"'

remote-logs-engine:
	$(REMOTE_SSH) docker service logs -f --tail 200 hive-engine

remote-logs-agent:
	$(REMOTE_SSH) docker service logs -f --tail 200 hive-agent

remote-logs-postgres:
	@echo "Tailing PostgreSQL HA logs (all pg nodes)..."
	@$(REMOTE_SSH) 'for svc in $$(docker service ls --filter label=hive.role=postgres-ha --format "{{.Name}}"); do \
	  docker service logs -f --tail 100 $$svc 2>&1 | sed "s/^/[$$svc] /" & \
	done; wait'

remote-logs-all:
	@echo "Tailing all hive services (Ctrl-C to stop)..."
	@$(REMOTE_SSH) 'docker service logs -f --tail 50 hive-manager 2>&1 | sed "s/^/[manager] /" & \
	 docker service logs -f --tail 50 hive-engine  2>&1 | sed "s/^/[engine]  /" & \
	 docker service logs -f --tail 50 hive-agent   2>&1 | sed "s/^/[agent]   /" & \
	 wait'

remote-status:
	@echo "=== Nodes ==="
	@$(REMOTE_SSH) docker node ls
	@echo ""
	@echo "=== Hive Services ==="
	@$(REMOTE_SSH) docker service ls --filter label=hive.managed=true
	@echo ""
	@echo "=== PostgreSQL HA Cluster ==="
	@$(REMOTE_SSH) 'docker service ls --filter label=hive.role=postgres-ha --format "table {{.Name}}\t{{.Mode}}\t{{.Replicas}}\t{{.Image}}" 2>/dev/null || echo "  (no HA postgres cluster)"'
	@echo ""
	@echo "=== Manager Tasks ==="
	@$(REMOTE_SSH) 'docker service ps hive-manager --no-trunc 2>/dev/null || true'
	@echo ""
	@echo "=== Engine Tasks ==="
	@$(REMOTE_SSH) 'docker service ps hive-engine --no-trunc 2>/dev/null || true'
	@echo ""
	@echo "=== Agent Tasks ==="
	@$(REMOTE_SSH) 'docker service ps hive-agent --no-trunc 2>/dev/null || true'

remote-ps:
	@echo "=== Nodes ==="
	@$(REMOTE_SSH) docker node ls
	@echo ""
	@echo "=== All Hive Containers ==="
	@$(REMOTE_SSH) 'docker service ps hive-manager hive-engine hive-agent --no-trunc 2>/dev/null || true'

remote-cleanup:
	@$(REMOTE_SSH) 'docker container prune -f && echo "Pruned stopped containers"'

remote-shell:
	@ssh -o StrictHostKeyChecking=no $(REMOTE_USER)@$(REMOTE_HOST)
