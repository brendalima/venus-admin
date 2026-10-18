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

Starts on `http://localhost:8080`. The API refuses to start without `DATABASE_URL_UNPOOLED` or `DATABASE_URL`, and in production also without the R2 settings, `IMAGE_BASE_URL`, `PUBLIC_URL`, `RESEND_API_KEY` and `MAIL_FROM`.

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
| `GET /forgot-password` | "Esqueci minha senha": asks for the email |
| `POST /forgot-password` | Emails a reset link (form: `email`); answers the same whether or not the account exists |
| `GET /reset-password?token=…` | The link from the email: new password form, or "this link no longer works" |
| `POST /reset-password` | Sets the new password and logs in (form: `token`, `password`, `confirm`) |
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

Server-rendered `html/template`, no build step. Every form is a plain HTML form that posts and redirects. JavaScript does two things: HTMX spares the list a reload when toggling "sold out", and `static/photos.js` runs the photo section of the product form (below). Without JavaScript everything still works, except reordering and removing photos.

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
| `POST /admin/products/{id}/delete` | Deletes it, and its photo files |
| `POST /admin/publish` | Starts the site's rebuild (below) |

Product form fields (`multipart/form-data`, since photos go with them): `name`, `price` (optional; reais as typed: `189`, `189,90`, `R$ 1.234,56`, `189.90`), `sizes` (repeated; `P`, `M`, `G`, `GG`), `description`, `details` (one per line), `sold_out` (checkbox), on edit `slug`, and the photos: `photos` (files), `photo_order` (the final order, cover first: `id:<image id>` for a photo the product has, `new:<n>` for the nth file) and `photo_remove` (repeated, photos to delete). Without `photo_order`, as from a browser without JavaScript, the files go after the existing photos.

- **Validation** reports every field at once, next to the field, and the form comes back exactly as typed. A server failure keeps the form too.
- **Prices** are typed in reais and converted to integer cents in one place, `internal/rest/price.go`. The price is optional for now: pieces can be catalogued before they are priced, and an empty price is stored as `0` ("Sem preço" in the list). A price that cannot be read is refused, never saved as zero.
- **The address (slug)** comes from the name on creation. Renaming a product does not change it, so shared links keep working; it can be changed by hand under "Opções avançadas".
- **Deleting** goes through a confirmation page that names the product and suggests marking it sold out instead.

### Photos

In the product form, under "Fotos", up to **12 per product**, each up to 15 MB. The first is the cover; "↑ Antes", "↓ Depois", "Usar como capa" and "Tirar" arrange them.

- **Nothing is saved until "Salvar alterações"** (or "Cadastrar peça"), decided with the owners' needs in mind: they arrange the photos and confirm once. Until then `static/photos.js` keeps the chosen files in the browser and shows them as previews; a notice says there are unsaved changes, and leaving the page asks first. Photos taken out wait under "Serão apagadas ao salvar", with "Desfazer".
- **All or nothing.** Saving sends the fields and the photos together. The service checks every field and processes every new photo before storing anything, so one refusal lists every problem; then it saves the product and its photo list in one transaction. A refusal changes nothing, and the page keeps the photos as they were arranged: `photos.js` only swaps in the messages.
- **A stale screen is refused.** If the photos changed elsewhere (another device) since the page was loaded, saving fails with a message instead of deleting a photo the person never saw.

- **What comes in:** JPEG, PNG or WebP, recognised by their first bytes, not by name or declared type. Anything else is refused with a message naming the file. The phone's file picker offers JPEG for HEIC photos, because the form accepts only these types.
- **What goes out:** two JPEGs, **1200 and 600 px wide** (never enlarged), keyed `produtos/<image id>-<width>.jpg`. Re-encoding drops all metadata, **GPS included**; first it applies the EXIF orientation, so phone photos come out upright, and keeps the colour profile, so Display P3 photos keep their colours.
- **Pure Go, no cgo:** resized by area averaging (`internal/storage/resample.go`), one photo at a time, each read from the upload's temporary file only when its turn comes, so a dozen phone photos never sit in memory together. A 12 MP photo takes ~0.3 s and ~24 MB on top of the process; `x/image/draw`'s kernels would need ~116 MB more, which the 256 MB machine does not have. JPEG instead of WebP costs ~25% in size; libvips could replace it behind `ImageProcessor` (see the end of the Dockerfile).
- **Storage:** Cloudflare R2 in production, through its S3 API, files marked `immutable`. In development without R2, photos go to `.media/` and the API serves them under `/media/`.
- **Deleting** a photo or a product removes the row first and the files after: a file left behind is only wasted space and is logged with its key; a row without its file would be a broken photo.
- Saving a product gets 10 minutes instead of the server's 30-second read timeout, since several photos over a phone connection take longer.
- `photos.js` is served at `/static/photos.js?v=<hash of its content>`, so browsers can cache it for good and still get a new version as soon as it changes.

### Publishing

Saving changes the catalogue here, not the site. The site is static and only picks the changes up when someone publishes:

1. The list shows **"Há alterações não publicadas"** whenever the catalogue changed after the last publish. Triggers move `site_state.last_changed_at` on every product and photo change (migration `00003`).
2. **"Publicar alterações"** is always on the list: highlighted while there are changes, plain otherwise. It calls the Cloudflare Pages deploy hook, then records the publish.
3. The answer: *"Publicação iniciada — o site atualiza em cerca de 1 minuto."* The build is not followed. If it fails, the badge stays on, which is the signal to try again.

The hook is called before the publish is recorded. If recording fails after a successful call, the badge stays on and the next try builds once more, which is harmless. The other order could mark a catalogue as published when the site never got it.

Without `DEPLOY_HOOK_URL` there is nothing to rebuild. Development then only logs, while production shows the badge and says publishing is not set up yet, with no button.

### The site's API

`GET /api/products` is what the site's build reads, once per publish. Visitors never reach it. It needs `Authorization: Bearer $API_TOKEN` and answers every product in catalogue order, with no pagination: the build needs all of them, and there are a few dozen.

```bash
curl -s -H "Authorization: Bearer $API_TOKEN" localhost:8080/api/products
# 200
# [
#   {
#     "id": "8a0e2f9e-4a8b-4c55-9d4c-6f1b7b0f2a11",
#     "slug": "biquini-aurora",
#     "name": "Biquíni Aurora",
#     "price_cents": 18990,
#     "sizes": ["P", "M"],
#     "images": [
#       {"url_600": "https://…/produtos/<image id>-600.jpg", "url_1200": "https://…/produtos/<image id>-1200.jpg"}
#     ],
#     "description": "Triângulo verde",
#     "details": ["Forro duplo"],
#     "sold_out": false
#   }
# ]

curl -s localhost:8080/api/products
# 401 {"error":"missing or invalid token"}
```

- A product without a price has **no `price_cents`**, so the site cannot show it as R$ 0,00. An empty `description` or `details` is left out too.
- `images` is always present, possibly empty, cover first.
- `Cache-Control: no-store`. In development without `API_TOKEN`, the route is off.

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

### Forgotten passwords

"Esqueci minha senha" on the login page emails a link through **Resend**, so the owners never need the developer to get back in.

- The link carries a random 256-bit token; the database keeps only its SHA-256 (`password_resets`). It works **once, for 1 hour**.
- The answer is the same whether or not the email has an account. The confirmation names the address, so a typo shows.
- At most **3 links per account per hour** and **5 requests per IP every 15 minutes**, so nobody can flood an inbox or spend the free sending quota.
- Setting a new password **logs out every other session**: `admin_users.password_version` goes up, and a session that logged in under an older version is turned away. The person who reset is logged straight in.
- The token travels in the query string, which the access log leaves out (it logs the path only), and the reset pages are `Cache-Control: no-store`.
- Links start with `PUBLIC_URL`, never with the request's `Host` header, which the client controls.
- In development, without `RESEND_API_KEY`, the email is written to the log instead, link included.

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

Photos follow the same split, with two R2 buckets and a token scoped to each, so a developer machine never holds credentials that write to production:

| R2 bucket | Used by | Public address | Credentials live in |
|---|---|---|---|
| `venusoasis-fotos` | the app on Fly | `https://fotos.venusoasis.com.br` | `fly secrets` |
| `venusoasis-fotos-dev` | `make run` on your machine | its `r2.dev` subdomain | `.env.local` |

Leaving the R2 variables out of `.env.local` keeps photos in `.media/` instead.

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
| `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET` | — | in production (`fly secrets`), all four together; in development, empty keeps photos on disk |
| `IMAGE_BASE_URL` | `http://localhost:$PORT/media` without R2 | with R2: the bucket's public address, `https` in production |
| `MEDIA_DIR` | `.media` | development without R2 only |
| `DEPLOY_HOOK_URL` | — | no; without it production does not offer publishing. A secret; `https` in production |
| `API_TOKEN` | — | in production, at least 32 characters (`openssl rand -hex 16`); in development, empty turns `/api/products` off |
| `PUBLIC_URL` | `http://localhost:$PORT` | in production, `https` (set in `fly.toml`) |
| `RESEND_API_KEY` | — | in production (`fly secrets`); in development, empty logs emails instead |
| `MAIL_FROM` | — | whenever `RESEND_API_KEY` is set; its domain must be verified in Resend |
| `TEST_DATABASE_URL` | — | tests only; integration tests skip without it |

New variables go into `.env-sample`.

## Deploy

Fly.io, region `gru` (São Paulo), a 256 MB machine that **stops when unused** and starts again on the next request.

Automatic on every merge into `master` (see Environments). One-time setup:

```bash
fly secrets set DATABASE_URL_UNPOOLED="$(neon cs production --project-id dry-water-16084849)"
fly secrets set R2_ACCOUNT_ID=… R2_ACCESS_KEY_ID=… R2_SECRET_ACCESS_KEY=… R2_BUCKET=… IMAGE_BASE_URL=https://…
fly secrets set API_TOKEN="$(openssl rand -hex 16)"   # the site's build needs the same value
fly secrets set DEPLOY_HOOK_URL=https://api.cloudflare.com/client/v4/pages/webhooks/deploy_hooks/…   # once the site is on Pages
fly secrets set RESEND_API_KEY=re_… MAIL_FROM="Vênus Oasis <…@…>"
fly tokens create deploy   # store the output as the GitHub secret FLY_API_TOKEN
```

`fly deploy` by hand still works for an emergency; it runs `/migrate`, then starts `/api`.

## Status

Server, configuration, middleware, health checks, graceful shutdown, separate migration binary, Docker, docker compose, fly.toml, CI. Neon connected, catalogue schema migrated (`products`, `product_images`, `site_state`), domain model and contracts defined, Postgres product repository with integration tests. Login, logout and sessions, with users created by `cmd/adduser`. Product screens: list, create, edit, sold-out toggle, delete. Photos: upload, resize, reorder, delete, stored in R2. Publishing: the pending-changes badge, the publish button and `/api/products`, with the deploy hook waiting for the site's Pages project. Password reset by email through Resend.

Next: the site's build reading `/api/products` (M4).
