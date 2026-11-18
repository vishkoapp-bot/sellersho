# Drup Platform — Go Backend Core (Modular Monolith)

This directory contains the production-grade Go backend foundation for the Drup dropshipping, warehouse, and fulfillment platform.

## Architecture

- **Pattern:** Modular Monolith with Clean Architecture & Domain Isolation
- **Language:** Go (1.23+)
- **Primary Database:** PostgreSQL 16 (Relational, ACID, strictly immutable double-entry ledger)
- **Cache & Fast Locks:** Redis 7 (Inventory hold redlock, ad click deduplication, OTP expiration)
- **Port:** `8080` (HTTP)

## Directory Structure

```
backend/
├── cmd/
│   └── server/
│       └── main.go                  # HTTP Entrypoint & Router
├── internal/
│   ├── config/
│   │   └── config.go                # Env & Runtime Configuration
│   ├── domain/
│   │   └── models.go                # Pure Domain Entities matching src/types/index.ts
│   └── pricing/
│       ├── pricing.go               # Canonical Pricing Engine (B = C + F, P = B + D)
│       └── pricing_test.go          # Unit Tests matching frontend pricing calculations
├── migrations/
│   ├── 000001_initial_schema.up.sql   # PostgreSQL 16 DDL Tables
│   └── 000001_initial_schema.down.sql # Rollback script
├── docker-compose.yml               # Local PostgreSQL & Redis infrastructure
├── Dockerfile                       # Multi-stage container build
├── go.mod                           # Go Module Definition
└── README.md
```

## Running Infrastructure

To start PostgreSQL 16 and Redis 7:

```bash
cd backend
docker-compose up -d postgres redis
```

SEC-01: the dev compose file holds no literal passwords, so the stack
needs its credentials set explicitly first (placeholders only - use
your own dev-only passwords):

```bash
export POSTGRES_PASSWORD='<choose-a-dev-password>'
export REDIS_PASSWORD='<choose-a-dev-password>'
# API inside compose:
export DATABASE_URL='postgres://drup_admin:<POSTGRES_PASSWORD>@postgres:5432/drup_db?sslmode=disable'
export REDIS_URL='redis://:<REDIS_PASSWORD>@redis:6379/0'
# ... or API on the host (go run): same passwords, localhost hosts.
```

Existing dev volumes were initialised with the retired dev password -
recreate them (`docker compose -f backend/docker-compose.yml down -v`)
or set the variables to the password the volumes were created with.
Full detail: `DEV-WORKFLOW.md`.

## Running the Server

```bash
cd backend
go run cmd/server/main.go
```

## Running Unit Tests

To run the pricing engine verification tests:

```bash
cd backend
go test -v ./internal/pricing/...
```
