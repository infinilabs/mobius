# Makefile for Mobius

.PHONY: build-frontend build-backend build-all build-sandbox serve test sanity clean \
        docker-up docker-up-postgres docker-up-elasticsearch docker-down docker-status \
        wipe-data bq-connection

# Variables
PORT=1983
BINARY_NAME=mobius-server
POSTGRES_CONTAINER=mobius-postgres
ES_CONTAINER=mobius-elasticsearch
DATA_DIR=$(shell pwd)/data
SANDBOX_IMAGE=mobius-agent:latest

# BigQuery media-tagging connection (override on the command line if needed).
# Defaults match conf.yaml's bigquery block / video_tagging.md §4.1 (project
# du-hast-mich, connection us.mobius_conn). The app reads the same connection
# from conf.yaml (bigquery.connection); these vars only drive `make bq-connection`.
BQ_PROJECT?=du-hast-mich
BQ_LOCATION?=us
BQ_CONNECTION?=mobius_conn

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

build-sandbox:
	@echo "==> Building agent sandbox image ($(SANDBOX_IMAGE))..."
	docker build -t $(SANDBOX_IMAGE) sandbox/
	@echo "==> Sandbox image built. Agents run commands inside this image."

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
		docker run --rm -v "$(DATA_DIR)/rdb:/data" alpine sh -c "chown 999:999 /data && chmod 700 /data"; \
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
		docker run --rm -v "$(DATA_DIR)/es:/data" alpine chown 1000:1000 /data; \
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

# --- BigQuery media tagging (§6 of video_tagging.md) ---

# One-time, idempotent setup of the BQ cloud-resource connection used by
# AI.GENERATE_TABLE for media tagging. Creates the connection (if absent) and
# grants its service agent the two roles it needs: Vertex AI (call Gemini) and
# GCS read (object table reads media). The dataset + remote model are created
# in-process by the app (EnsureTaggingInfra), so this target stops at the
# connection + IAM — the part that needs admin/CLI perms. Override defaults with
# e.g. make bq-connection BQ_PROJECT=my-proj BQ_CONNECTION=my_conn
bq-connection:
	@command -v bq >/dev/null     || { echo "ERROR: 'bq' CLI not found — install the Google Cloud SDK."; exit 1; }
	@command -v gcloud >/dev/null || { echo "ERROR: 'gcloud' CLI not found — install the Google Cloud SDK."; exit 1; }
	@echo "==> [1/3] Ensuring connection $(BQ_LOCATION).$(BQ_CONNECTION) in project $(BQ_PROJECT)..."
	@if bq --project_id=$(BQ_PROJECT) show --connection $(BQ_LOCATION).$(BQ_CONNECTION) >/dev/null 2>&1; then \
		echo "    connection already exists — skipping create."; \
	else \
		bq --project_id=$(BQ_PROJECT) mk --connection --location=$(BQ_LOCATION) \
			--connection_type=CLOUD_RESOURCE $(BQ_CONNECTION); \
	fi
	@echo "==> [2/3] Reading connection service account..."
	@SA=$$(bq --project_id=$(BQ_PROJECT) show --format=prettyjson --connection $(BQ_LOCATION).$(BQ_CONNECTION) \
		| grep serviceAccountId | cut -d '"' -f 4); \
	if [ -z "$$SA" ]; then echo "ERROR: could not read the connection's service account."; exit 1; fi; \
	echo "    service account: $$SA"; \
	echo "==> [3/3] Granting roles on $(BQ_PROJECT) (idempotent)..."; \
	gcloud projects add-iam-policy-binding $(BQ_PROJECT) \
		--member="serviceAccount:$$SA" --role=roles/aiplatform.user --condition=None >/dev/null \
		|| { echo "ERROR: failed to grant roles/aiplatform.user to $$SA (needs a project Owner/IAM-Admin account)."; \
		     echo "  Run as such an account:"; \
		     echo "    gcloud projects add-iam-policy-binding $(BQ_PROJECT) --member=serviceAccount:$$SA --role=roles/aiplatform.user --condition=None"; exit 1; }; \
	gcloud projects add-iam-policy-binding $(BQ_PROJECT) \
		--member="serviceAccount:$$SA" --role=roles/storage.objectViewer --condition=None >/dev/null \
		|| { echo "ERROR: failed to grant roles/storage.objectViewer to $$SA (needs a project Owner/IAM-Admin account)."; \
		     echo "  Run as such an account:"; \
		     echo "    gcloud projects add-iam-policy-binding $(BQ_PROJECT) --member=serviceAccount:$$SA --role=roles/storage.objectViewer --condition=None"; exit 1; }; \
	echo "==> Done. $(BQ_LOCATION).$(BQ_CONNECTION) is ready (Vertex AI + GCS read granted)."
	@echo "    Note: the app's own BQ service account still needs roles/bigquery.jobUser +"
	@echo "    edit on the target dataset (usually already set for the token/event pipeline)."

# Wipe ALL local data stores so the next boot starts clean. This removes the
# containers and erases Postgres, Elasticsearch, and on-disk project files —
# the three stores that must stay in sync. Cloud stores (GCS/BigQuery) are NOT
# touched. Requires an explicit "yes" confirmation.
wipe-data:
	@echo ""
	@echo "  ⚠  WARNING: this PERMANENTLY DELETES all local Mobius data:"
	@echo "       • PostgreSQL   ($(DATA_DIR)/rdb)"
	@echo "       • Elasticsearch ($(DATA_DIR)/es)"
	@echo "       • Project files (./projects, incl. archived/ and deleted/)"
	@echo "     The $(POSTGRES_CONTAINER) and $(ES_CONTAINER) containers will be removed."
	@echo "     Cloud stores (GCS / BigQuery) are NOT affected."
	@echo ""
	@read -p "  Type 'yes' to wipe everything, anything else to abort: " ans; \
	if [ "$$ans" != "yes" ] && [ "$$ans" != "y" ]; then \
		echo "==> Aborted. No data was deleted."; \
		exit 0; \
	fi; \
	echo "==> Removing containers..."; \
	docker rm -f $(POSTGRES_CONTAINER) $(ES_CONTAINER) 2>/dev/null || true; \
	echo "==> Wiping Postgres + Elasticsearch data (root container; files are container-owned)..."; \
	docker run --rm -v "$(DATA_DIR):/data" alpine sh -c "rm -rf /data/rdb /data/es"; \
	echo "==> Wiping local project files..."; \
	rm -rf projects; \
	echo "==> Done. All local data wiped. Run 'make serve' to start fresh."
