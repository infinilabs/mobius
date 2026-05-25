# Makefile for Mobius

.PHONY: build-frontend build-backend build-all serve test sanity clean \
        docker-up docker-up-postgres docker-up-elasticsearch docker-down docker-status

# Variables
PORT=1983
BINARY_NAME=mobius-server
POSTGRES_CONTAINER=mobius-postgres
ES_CONTAINER=mobius-elasticsearch
DATA_DIR=$(shell pwd)/data

# --- Build ---

build-frontend:
	@echo "==> Building frontend..."
	cd frontend && npm install && npm run build
	@echo "==> Moving static files to backend..."
	mkdir -p backend/static
	rm -rf backend/static/*
	cp -r frontend/dist/* backend/static/

build-backend:
	@echo "==> Building Go backend..."
	mkdir -p bin
	cd backend && go build -o ../bin/$(BINARY_NAME) .

build-all: build-frontend build-backend
	@echo "==> Mobius built successfully."

serve: docker-up build-all
	@if [ ! -f conf.yaml ]; then \
		echo "==> conf.yaml not found, copying from template..."; \
		cp conf.yaml.template conf.yaml; \
	fi
	@echo "==> Launching Mobius on port $(PORT)..."
	./bin/$(BINARY_NAME)

test:
	@echo "==> Running tests..."
	cd backend && go test -v ./...
	cd frontend && npm run test

sanity:
	@echo "==> [1/5] Go vet..."
	cd backend && go vet ./...
	@echo "==> [2/5] Go build..."
	cd backend && go build ./...
	@echo "==> [3/5] Go test (race detector)..."
	cd backend && go test -race -count=1 ./...
	@echo "==> [4/5] TypeScript type check..."
	cd frontend && npx tsc --noEmit
	@echo "==> [5/5] Frontend lint..."
	cd frontend && npx eslint src --max-warnings 0
	@echo "==> Sanity check passed."

clean:
	@echo "==> Cleaning up..."
	rm -f bin/$(BINARY_NAME)
	rm -rf frontend/dist
	rm -rf backend/static

# --- Docker Infrastructure ---

docker-up: docker-up-postgres docker-up-elasticsearch

docker-up-postgres:
	@mkdir -p $(DATA_DIR)/rdb
	@if docker ps -a -q -f name=^$(POSTGRES_CONTAINER)$$ | grep -q .; then \
		if ! docker ps -q -f name=^$(POSTGRES_CONTAINER)$$ | grep -q .; then \
			echo "==> Starting existing PostgreSQL container..."; \
			docker start $(POSTGRES_CONTAINER); \
		else \
			echo "==> PostgreSQL already running."; \
		fi; \
	else \
		echo "==> Creating PostgreSQL container..."; \
		docker run -d --name $(POSTGRES_CONTAINER) \
			-e POSTGRES_USER=mobius \
			-e POSTGRES_PASSWORD=mobius \
			-e POSTGRES_DB=mobius \
			-p 5432:5432 \
			-v $(DATA_DIR)/rdb:/var/lib/postgresql \
			postgres:18; \
	fi

docker-up-elasticsearch:
	@mkdir -p $(DATA_DIR)/es
	@if docker ps -a -q -f name=^$(ES_CONTAINER)$$ | grep -q .; then \
		if ! docker ps -q -f name=^$(ES_CONTAINER)$$ | grep -q .; then \
			echo "==> Starting existing Elasticsearch container..."; \
			docker start $(ES_CONTAINER); \
		else \
			echo "==> Elasticsearch already running."; \
		fi; \
	else \
		echo "==> Creating Elasticsearch container..."; \
		docker run -d --name $(ES_CONTAINER) \
			-e discovery.type=single-node \
			-e xpack.security.enabled=false \
			-e "ES_JAVA_OPTS=-Xms1g -Xmx1g" \
			-p 9200:9200 \
			-v $(DATA_DIR)/es:/usr/share/elasticsearch/data \
			elasticsearch:9.3.4; \
	fi

docker-down:
	@echo "==> Stopping Mobius containers..."
	@docker stop $(POSTGRES_CONTAINER) $(ES_CONTAINER) 2>/dev/null || true
	@echo "==> Containers stopped. Data preserved in $(DATA_DIR)/"

docker-destroy:
	@echo "==> Removing Mobius containers (data preserved)..."
	@docker rm -f $(POSTGRES_CONTAINER) $(ES_CONTAINER) 2>/dev/null || true

docker-status:
	@docker ps -a -f name=mobius --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
