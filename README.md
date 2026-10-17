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

Hexagonal (ports and adapters), following [AGENTS.md](AGENTS.md). Dependencies point inward: handlers know the services through interfaces, services know the persistence and external adapters through interfaces, and the domain knows nothing.

```
cmd/
  api/                   HTTP server: main.go (wiring + routes), server.go
  migrate/               applies schema migrations, then exits
  adduser/               gives a person access to the admin
adapters/                config from the environment, database connection,
                         embedded goose migrations (adapters/migrations/)
internal/
  core/
    domain/              entities (Product, Image, User), Slugify,
                         validation, domain errors — stdlib only
    services/            business rules + contracts.go (what handlers use)
  repository/            SQL over database/sql + contracts.go
                         (Product: catalogue CRUD, pending-changes state;
                         User; Session: the login session store)
  storage/               external services (R2, image processing,
                         Cloudflare deploy hook) — contracts only for now
  rest/                  HTTP handlers, middleware, sessions, and the
                         screens (html/template, embedded with go:embed)
```

The layer rules are enforced by `internal/architecture_test.go`: a forbidden import, or an `os.Getenv` inside `internal/`, fails the suite.

Contracts declared ahead of their implementation: `ImageStore`, `ImageProcessor`, `SitePublisher`.

## Running

Needs Go 1.27 (pinned in `.tool-versions`, via asdf) and a database URL.

```bash
cp .env-sample .env
make migrate
make run
```

Starts on `http://localhost:8080`. The API refuses to start without `DATABASE_URL_UNPOOLED` or `DATABASE_URL`.

In development `.env.local` (written by `neon init`) is loaded automatically. A variable already exported in the shell still wins.

With Docker, `docker compose up` starts a local Postgres, runs the migration and then the API — it never touches Neon.

| Command | What it does |
|---|---|
| `make run` | API in development mode |
| `make migrate` | Apply pending migrations |
| `make test` | Tests with the race detector |
| `make check` | Everything CI runs: gofmt, vet, staticcheck, tests, govulncheck |
| `make cover` | Coverage report as HTML |
| `make adduser` | Give someone access to the admin |
| `make build` | Binaries into `bin/` (`api`, `migrate`, `adduser`) |

## Endpoints

| Route | Purpose |
|---|---|
| `GET /healthz` | Liveness — does **not** touch the database |
| `GET /readyz` | Readiness — does touch the database |
| `GET /` | Redirects to `/admin` |
| `GET /login` | Login page |
| `POST /login` | Logs in (form: `email`, `password`) |
| `POST /logout` | Logs out |
| `GET /admin` | Admin panel (placeholder) — requires login |

Splitting the two checks is deliberate. Fly uses `/healthz` to decide whether the machine is healthy, and Neon's free tier suspends when idle: if liveness depended on the database, the machine would be marked unhealthy every time Postgres took a nap.

```bash
curl -i localhost:8080/healthz
# 200 {"status":"ok"}

curl -i localhost:8080/readyz
# 200 {"status":"ok","database":"ok"}
# 503 {"error":"database unreachable"}
```

Errors from the JSON endpoints always come back as `{"error": "<message>"}`. The admin screens are HTML, with their messages in Portuguese.

## Access

There is **no public signup**. Someone gets access when the developer runs `cmd/adduser`, which asks for the password twice without echoing it:

```bash
make adduser                                          # locally, against .env.local
fly ssh console -C "/adduser -email ana@exemplo.com"  # in production
```

- Passwords are hashed with **argon2id** (19 MiB, 2 passes — OWASP's first option; the 256 MB machine is the limit). Parameters are stored in the hash, so they can be raised later.
- Sessions are handled by `alexedwards/scs`, stored in Postgres (`sessions` table) as a hash of the token. Cookie `venus_session`: `HttpOnly`, `SameSite=Lax`, `Secure` in production. They last 30 days and end after 7 without use. The token is renewed on login.
- A wrong email and a wrong password get the same message and take the same time.
- Login is limited to **10 attempts per IP every 15 minutes**, counted in memory. Behind Fly the IP comes from `Fly-Client-IP` (`CLIENT_IP_HEADER`).
- CSRF: `http.CrossOriginProtection` rejects state-changing requests from other sites, together with the `SameSite=Lax` cookie. There are no tokens to put in forms.
- Expired sessions are deleted on each successful login rather than on a timer, so nothing wakes Neon up on its own.

## Database

Postgres 18 on **Neon**, project `venus`, region `aws-sa-east-1` (São Paulo) — the same region as the app on Fly.

Migrations live in `adapters/migrations/`, embedded in the binary with `go:embed`, and are applied by the separate `cmd/migrate` binary — never by the API on startup. On Fly it runs as the `release_command`, once per deploy, before the new version starts.

Whether the site has unpublished changes comes from `site_state`: triggers on `products` and `product_images` stamp `last_changed_at` on every insert, update and delete (migration `00003`), and publishing stamps `last_published_at`. A per-product `updated_at` alone would miss deleted products and photo changes.

The migrator holds a Postgres advisory lock, so two of them started at once take turns.

IDs are UUIDs (v4), generated by the services with `uuid.New()` rather than by the database, so nobody can walk the catalogue by counting. The columns deliberately have no default; `adapters/database_test.go` checks that.

### Why the direct connection, not the pooled one

Neon exposes both: `DATABASE_URL` goes through PgBouncer and `DATABASE_URL_UNPOOLED` goes direct. The app uses the **direct** one: `database/sql` already keeps its own pool, and pgx's prepared statement cache breaks under PgBouncer's transaction pooling. With one process and low concurrency there is nothing to gain in exchange.

Idle connections close after two minutes on purpose: Neon's compute suspends when idle, and holding a connection open would keep it awake, burning free-tier hours while the admin sits unused.

```bash
make test                            # integration tests skip without a database
go test ./adapters/ -v                # with .env.local present, they really run
```

## Configuration

Everything through environment variables — what Fly expects (`fly secrets`), and it keeps credentials out of the repository.

| Variable | Default | Required |
|---|---|---|
| `APP_ENV` | `dev` | no (`dev` or `prod`) |
| `PORT` | `8080` | no |
| `DATABASE_URL_UNPOOLED` | — | one of the two (preferred) |
| `DATABASE_URL` | — | one of the two |
| `CLIENT_IP_HEADER` | — | no; `Fly-Client-IP` on Fly (set in `fly.toml`) |

New variables go into `.env-sample`.

## Deploy

Fly.io, region `gru` (São Paulo), a 256 MB machine that **stops when unused** and starts again on the next request.

```bash
fly launch --no-deploy   # first time
fly secrets set DATABASE_URL_UNPOOLED="..."
fly deploy               # runs /migrate, then starts /api
```

## Status

Server, configuration, middleware, health checks, graceful shutdown, separate migration binary, Docker, docker compose, fly.toml, CI. Neon connected, catalogue schema migrated (`products`, `product_images`, `site_state`), domain model and contracts defined, Postgres product repository with integration tests. Login, logout and sessions, with users created by `cmd/adduser`.

Next: the CRUD screens, then password reset by email.
