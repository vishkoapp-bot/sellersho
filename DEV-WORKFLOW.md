# Seler-Sho Development Workflow

To start the complete environment (PostgreSQL 16, Redis 7, Go API Backend on port 8080, and Vite Frontend on port 5173):

```bash
# First set the dev-stack credentials (see "Dev-stack credentials"
# below) — the compose file holds no literal passwords since SEC-01.
pnpm run dev:all
# or
npm run dev:all
```

This single command:
1. Spawns Docker containers (`drup_postgres`, `drup_redis`, `drup_go_api`) defined in `backend/docker-compose.yml`.
2. Starts the Vite SPA with automatic proxy routing `/api` -> `http://localhost:8080`.

### Dev-stack credentials (SEC-01 — required, no defaults)

Since SEC-01 the dev compose file contains **no literal passwords** and
the server's dev fallbacks are passwordless localhost URLs, so the
docker-compose stack does **not** work with an empty environment: the
API container would fail DB/Redis auth. Set these four variables
explicitly before `docker compose up` (use your own dev-only
passwords — placeholders below, never a production value):

```bash
export POSTGRES_PASSWORD='<choose-a-dev-password>'
export REDIS_PASSWORD='<choose-a-dev-password>'

# API running INSIDE compose (service DNS names postgres / redis):
export DATABASE_URL='postgres://drup_admin:<POSTGRES_PASSWORD>@postgres:5432/drup_db?sslmode=disable'
export REDIS_URL='redis://:<REDIS_PASSWORD>@redis:6379/0'

# ... or API running ON THE HOST (go run cmd/server/main.go), same
# passwords, localhost hosts instead:
export DATABASE_URL='postgres://drup_admin:<POSTGRES_PASSWORD>@localhost:5432/drup_db?sslmode=disable'
export REDIS_URL='redis://:<REDIS_PASSWORD>@localhost:6379/0'
```

Pick the pair that matches how you run the API: the compose-network
hostnames (`postgres` / `redis`) only resolve inside the compose
network; a host-run server must use `localhost`.

Existing dev volumes were initialised with the retired dev password.
After the SEC-01 change, either recreate them:

```bash
docker compose -f backend/docker-compose.yml down -v
```

or set `POSTGRES_PASSWORD` / `REDIS_PASSWORD` to the password the
volumes were created with. (There is no `backend/.env.example` in the
tree — the export commands above are the source of truth.)

### Health Check Endpoints:
- Go Core API: `http://localhost:8080/api/health`
- Vite Frontend: `http://localhost:5173/`

### Seeder CLI:
To re-seed or reset test fixtures in local/dev environment:
```bash
docker run --rm --env-file backend/.env -v "D:\drup\backend:/app" -w /app --network backend_default golang:1.23-alpine go run cmd/seed/main.go -action=seed -env=local -db="postgres://drup_admin:<POSTGRES_PASSWORD>@drup_postgres:5432/drup_db?sslmode=disable"
```
