# Seler-Sho Development Workflow

To start the complete environment (PostgreSQL 16, Redis 7, Go API Backend on port 8080, and Vite Frontend on port 5173):

```bash
# First copy backend/.env.example to backend/.env and replace every placeholder.
pnpm run dev:all
# or
npm run dev:all
```

This single command:
1. Spawns Docker containers (`drup_postgres`, `drup_redis`, `drup_go_api`) defined in `backend/docker-compose.yml`.
2. Starts the Vite SPA with automatic proxy routing `/api` -> `http://localhost:8080`.

### Health Check Endpoints:
- Go Core API: `http://localhost:8080/api/health`
- Vite Frontend: `http://localhost:5173/`

### Seeder CLI:
To re-seed or reset test fixtures in local/dev environment:
```bash
docker run --rm --env-file backend/.env -v "D:\drup\backend:/app" -w /app --network backend_default golang:1.23-alpine go run cmd/seed/main.go -action=seed -env=local -db="postgres://drup_admin:drup_secure_password_2026@drup_postgres:5432/drup_db?sslmode=disable"
```
