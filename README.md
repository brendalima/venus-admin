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
                         screens (html/template, embedded with go:embed);
                         rest/static/ holds the vendored HTMX
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
| `GET /static/{file}` | Embedded static files (HTMX), cached for good: each name carries its version |

Splitting the two checks is deliberate. Fly uses `/healthz` to decide whether the machine is healthy, and Neon's free tier suspends when idle: if liveness depended on the database, the machine would be marked unhealthy every time Postgres took a nap.

```bash
curl -i localhost:8080/healthz
# 200 {"status":"ok"}

curl -i localhost:8080/readyz
# 200 {"status":"ok","database":"ok"}
# 503 {"error":"database unreachable"}
```

Errors from the JSON endpoints always come back as `{"error": "<message>"}`. The admin screens are HTML, with their messages in Portuguese.

### The admin screens

Server-rendered `html/template`, no build step. Every form is a plain HTML form that posts and redirects, so the admin works with JavaScript off; HTMX only spares the list a reload when toggling "sold out".

Every route here requires login; without it, the browser is sent to `/login`.

| Route | Purpose |
|---|---|
| `GET /admin` | List of products, with a quick "sold out" toggle |
| `GET /admin/products/new` | Form for a new product |
| `POST /admin/products` | Creates a product |
| `GET /admin/products/{id}` | Edit form |
| `POST /admin/products/{id}` | Saves the edits |
| `POST /admin/products/{id}/sold-out` | Sets sold out (form: `sold_out=true\|false`); answers the list row to HTMX |
| `GET /admin/products/{id}/delete` | Asks for confirmation, naming the product |
| `POST /admin/products/{id}/delete` | Deletes it |

Product form fields: `name`, `price` (optional; reais as typed: `189`, `189,90`, `R$ 1.234,56`, `189.90`), `sizes` (repeated; `P`, `M`, `G`, `GG`), `description`, `details` (one per line), `sold_out` (checkbox), and on edit `slug`.

- **Validation** reports every field at once, next to the field, and the form comes back exactly as typed. A server failure keeps the form too.
- **Prices** are typed in reais and converted to integer cents in one place, `internal/rest/price.go`. The price is optional for now: pieces can be catalogued before they are priced, and an empty price is stored as `0` ("Sem preço" in the list). A price that cannot be read is refused, never saved as zero.
- **The address (slug)** comes from the name on creation. Renaming a product does not change it, so shared links keep working; it can be changed by hand under "Opções avançadas".
- **Deleting** goes through a confirmation page that names the product and suggests marking it sold out instead.

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

## Environments

Three Neon branches, one per purpose:

| Neon branch | Used by | Connection string lives in |
|---|---|---|
| `production` | the app on Fly | `fly secrets` (`DATABASE_URL_UNPOOLED`) |
| `dev` | `make run`, `make migrate`, `make adduser` on your machine | `.env.local` (`DATABASE_URL`, `DATABASE_URL_UNPOOLED`) |
| `test` | the integration tests, migrated from scratch by the tests | `.env.local` (`TEST_DATABASE_URL`) |

`test` started schema-only and holds no production data. Integration tests read **only** `TEST_DATABASE_URL`, so they cannot reach `dev` or `production` by accident.

Shipping to production is a pull request merged into `master`: CI runs on the merge, and `.github/workflows/fly-deploy.yml` deploys only once CI has passed. `fly deploy` runs `/migrate` first (`release_command`), so a migration that fails stops the deploy.

## Configuration

Everything through environment variables — what Fly expects (`fly secrets`), and it keeps credentials out of the repository.

| Variable | Default | Required |
|---|---|---|
| `APP_ENV` | `dev` | no (`dev` or `prod`) |
| `PORT` | `8080` | no |
| `DATABASE_URL_UNPOOLED` | — | one of the two (preferred) |
| `DATABASE_URL` | — | one of the two |
| `CLIENT_IP_HEADER` | — | no; `Fly-Client-IP` on Fly (set in `fly.toml`) |
| `TEST_DATABASE_URL` | — | tests only; integration tests skip without it |

New variables go into `.env-sample`.

## Deploy

Fly.io, region `gru` (São Paulo), a 256 MB machine that **stops when unused** and starts again on the next request.

Automatic on every merge into `master` (see Environments). One-time setup:

```bash
fly secrets set DATABASE_URL_UNPOOLED="$(neon cs production --project-id dry-water-16084849)"
fly tokens create deploy   # store the output as the GitHub secret FLY_API_TOKEN
```

`fly deploy` by hand still works for an emergency; it runs `/migrate`, then starts `/api`.

## Status

Server, configuration, middleware, health checks, graceful shutdown, separate migration binary, Docker, docker compose, fly.toml, CI. Neon connected, catalogue schema migrated (`products`, `product_images`, `site_state`), domain model and contracts defined, Postgres product repository with integration tests. Login, logout and sessions, with users created by `cmd/adduser`. Product screens: list, create, edit, sold-out toggle, delete — without photos yet.

Next: password reset by email.
