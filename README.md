# fieldkit-backend

Go + Gin API for Fieldkit, the field verification demo from the DevFest Akure 2026 workshop *Designing Offline Enabled Data Intensive Web Apps: Web Workers, Cache Strategy, Storage Tiers, Sync, Queues, and Runtime Observability*.

Frontend: [fieldkit-frontend](https://github.com/spiderocious/fieldkit-frontend). Sibling demo: [wallet-backend](https://github.com/spiderocious/wallet-backend).

Live: `https://fieldkit-api-production.up.railway.app` (health at `/health`).

## What it does

| Route | Purpose |
|---|---|
| `POST /api/v1/auth/login` | phone + 4-digit PIN → bearer token |
| `GET /api/v1/visits`, `POST /api/v1/visits` | cursor-paged list; create with a device-generated id and an `Idempotency-Key` |
| `PUT /api/v1/visits/:id` | partial update with `base_version`; 409 `version_conflict` carries the server's copy |
| `POST /api/v1/visits/:id/photos` | multipart upload, 2 MB cap, content-hashed, served back immutable |
| `GET /api/v1/visits/stats` | totals and per-agent counts for the ops view |
| `POST /api/v1/telemetry` | batches of device events |
| `GET /api/v1/ping` | the online probe: no auth, no cache, no chaos |
| `GET/POST /__control` | demo only (`DEMO_CONTROL=true`): make the API fail on demand |

Every write under `/api/v1` accepts `Idempotency-Key`. The key, caller, endpoint and a body hash are stored in the same transaction as the write; a replay returns the stored response byte for byte, and a different body under the same key is a 422.

## Run

```sh
cp .env.example .env            # edit DATABASE_URL and JWT_SECRET
go run ./cmd/migrate            # creates the database if missing, applies migrations
go run ./cmd/seed               # 3 agents (PIN 1234), 20 visits; idempotent
go run ./cmd/api                # :8090 by default
```

Verify: `go test ./... -count=1` (contract tests on an embedded Postgres, no Docker), `golangci-lint run ./...`.

## Deploy

`Dockerfile` builds `api`, `migrate` and `seed`; `entrypoint.sh` runs them in that order. Set `DATABASE_URL`, `JWT_SECRET` (32+ chars), `CORS_ORIGIN` (comma-separated browser origins), `DATA_DIR`, and `DEMO_CONTROL`. Uploaded photos live on the container disk; add a volume at `DATA_DIR` for anything beyond a demo.

## Layout and rules

See [docs/conventions.md](docs/conventions.md): feature folders, typed domain errors, one response package, services that never see `*gin.Context`, cursor pagination, idempotency in the write's transaction.
