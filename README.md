# Mobius

A web application scaffold built with a Go backend and React frontend, designed to connect to Google Cloud Platform.

## Tech Stack

- **Backend**: Go (net/http, slog, lumberjack for log rotation, YAML config)
- **Frontend**: React 19, TypeScript, Vite, Tailwind CSS 4, Axios, ECharts, Lucide Icons
- **Data**: PostgreSQL (Docker), Elasticsearch (Docker), BigQuery (GCP)
- **Build**: Makefile + Docker

## Quick Start

```bash
# 1. Copy config template
cp conf.yaml.template conf.yaml
# Edit conf.yaml with your settings

# 2. Build and run (starts Docker containers + builds + launches server)
make serve
```

The server starts on **port 1983** by default.

## Configuration

Edit `conf.yaml` or use the **Settings** page in the web UI (changes persist to `conf.yaml`):

```yaml
server:
  port: 1983
  mode: "debug"

postgres:
  host: "localhost"
  port: 5432
  user: "mobius"
  password: "mobius"
  dbname: "mobius"

elasticsearch:
  url: "http://localhost:9200"

bigquery:
  project_id: "your-gcp-project-id"
  dataset: "mobius"
  credentials_path: ""  # leave empty to use ADC
```

### Data Backends

| Service | Purpose | Storage |
|---------|---------|---------|
| **PostgreSQL** | Business data (ad accounts, tasks, campaigns) | `data/rdb/` (Docker volume) |
| **Elasticsearch** | Creatives index and search | `data/es/` (Docker volume) |
| **BigQuery** | Analytical data (reporting, metrics) | GCP (cloud) |

Data directories are mounted from `data/` so Docker container restarts do not cause data loss.

## Build Commands

| Command | Description |
|---------|-------------|
| `make build-all` | Build frontend + backend |
| `make serve` | Start Docker + build all + launch server |
| `make docker-up` | Start PostgreSQL + Elasticsearch containers |
| `make docker-down` | Stop containers (data preserved) |
| `make docker-destroy` | Remove containers (data still preserved) |
| `make docker-status` | Show container status |
| `make clean` | Remove build artifacts |

## Development

For frontend development with hot reload:

```bash
# Start infrastructure
make docker-up

# Terminal 1: Start the Go backend
cd backend && go run .

# Terminal 2: Start Vite dev server (proxies /api to :1983)
cd frontend && npm run dev
```

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/health` | Health check |
| GET | `/api/config` | Project config (public) |
| GET | `/api/settings` | Full infrastructure settings |
| PUT | `/api/settings` | Update settings (persists to conf.yaml) |
| GET | `/api/conversations` | List all conversations (summary) |
| POST | `/api/conversations` | Create a new conversation |
| GET | `/api/conversations/{id}` | Get conversation with full message history |
| PUT | `/api/conversations/{id}` | Rename a conversation |
| DELETE | `/api/conversations/{id}` | Delete a conversation |
| POST | `/api/chat` | Send message and stream AI response (SSE) |
| POST | `/api/upload` | Upload a file (multipart) |

## Logging

Logs are written to the `logs/` directory in JSON format with automatic rotation:

- `logs/server.log` — Application logs
- `logs/access.log` — HTTP request logs

Log rotation: 32MB max size, 3 backups, 28-day max age, gzip compressed.
