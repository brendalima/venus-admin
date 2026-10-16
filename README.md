# Vênus Oasis — Admin

Backend and product catalogue admin for [venus-site](../venus-site).

**The public site stays static.** This service does not serve visitors: it hosts the admin and feeds the site's build. If it is down, asleep or mid-deploy, the shop stays online.

```
admin saves → clicks "Publicar"
   → Cloudflare Pages deploy hook
   → the site build fetches /api/products
   → static site republished (~1 min)
```

> Code, comments and commits are in English. **User-facing strings stay in Portuguese** — the people running this admin are Brazilian.

## Architecture

Hexagonal (ports and adapters). Dependencies point inward: adapters know the core, never the reverse.

```
cmd/server/              composition root — the only place that wires
                         adapters into the core

internal/
  catalog/               THE CORE. Domain model, rules and the ports it
                         needs. Imports nothing from this project and no
                         driver or framework.
    product.go             entities, Slugify, View
    errors.go              domain errors and validation
    ports.go               Repository, ImageStore, ImageProcessor,
                           SitePublisher — interfaces the core owns

  adapter/
    web/                 driving adapter: HTTP, routing, middleware

  platform/
    config/              configuration from the environment
    database/            pgx pool and goose migrations
```

Ports live in the core so the core owns the contract and adapters depend on it. That also happens to match Go's own habit of declaring interfaces where they are consumed.

The rule is enforced by tests, not by discipline alone — see `internal/catalog/architecture_test.go`. Add a stray import to the core and the suite fails.

Adapters still to come: `postgres` (Repository), `r2` (ImageStore), `vips` (ImageProcessor), `cloudflare` (SitePublisher).

## Running

Needs Go 1.27 (pinned in `.tool-versions`, via asdf).

```bash
cp .env.example .env
make run
```

Starts on `http://localhost:8080`. With no database configured the server still boots and `/readyz` says so — handy for working on screens without Postgres.

In development `.env.local` (written by `neon init`) is loaded automatically. A variable already exported in the shell still wins.

| Command | What it does |
|---|---|
| `make run` | Development server |
| `make test` | Tests with the race detector |
| `make check` | Everything CI runs: gofmt, vet, tests |
| `make cover` | Coverage report as HTML |
| `make build` | Binary into `bin/server` |

## Endpoints

| Route | Purpose |
|---|---|
| `GET /healthz` | Liveness — does **not** touch the database |
| `GET /readyz` | Readiness — does touch the database |
| `GET /admin` | Admin panel (placeholder) |

Splitting the two is deliberate. Fly uses `/healthz` to decide whether the machine is healthy, and Neon's free tier suspends when idle: if liveness depended on the database, the machine would be marked unhealthy every time Postgres took a nap.

## Database

Postgres 18 on **Neon**, project `venus`, region `aws-sa-east-1` (São Paulo) — the same region as the app on Fly.

Migrations live in `internal/platform/database/migrations/`, embedded in the binary with `go:embed`, and run at startup. With a single machine that is safe and avoids a separate deploy step; with more than one instance it becomes a `release_command` in `fly.toml` so two machines do not migrate at once.

### Why the direct connection, not the pooled one

Neon exposes both: `DATABASE_URL` goes through PgBouncer and `DATABASE_URL_UNPOOLED` goes direct. The app uses the **direct** one, because pgx already keeps its own pool — stacking it on transaction pooling breaks the prepared statement cache, and with one process and low concurrency there is nothing to gain in exchange.

The pool sets `MinConns = 0` on purpose: Neon's compute suspends when idle, and holding a connection open would keep it awake, burning free-tier hours while the admin sits unused.

```bash
make test                            # integration tests skip without a database
go test ./internal/platform/database/ -v   # with .env.local present, they really run
```

## Configuration

Everything through environment variables — what Fly expects (`fly secrets`), and it keeps credentials out of the repository.

| Variable | Default | Required |
|---|---|---|
| `APP_ENV` | `dev` | no (`dev` or `prod`) |
| `PORT` | `8080` | no |
| `DATABASE_URL_UNPOOLED` | — | yes (preferred) |
| `DATABASE_URL` | — | used if the above is absent |

## Deploy

Fly.io, region `gru` (São Paulo), a 256 MB machine that **stops when unused** and starts again on the next request.

```bash
fly launch --no-deploy   # first time
fly secrets set DATABASE_URL_UNPOOLED="..."
fly deploy
```

## Status

Server, configuration, middleware, health checks, graceful shutdown, Docker, fly.toml, CI. Neon connected, catalogue schema migrated (`products`, `product_images`, `site_state`), domain model and ports defined.

Next: the Postgres adapter, authentication and the CRUD screens.
