# Handoff — Vênus Oasis Admin

For whoever picks up the next milestone. Everything below was verified against the repo, not recalled.

Last verified: 2026-10-16 · commits `505c728` (pushed) and `bfc6058` (local only, not pushed).

---

## 1. What this is, in one paragraph

A Go backend and admin panel for a small Brazilian swimwear brand. The owners are not technical and will use the admin **without the developer in the loop** — that constraint drives most design decisions here.

The public storefront is a separate repo (`../venus-site`): a static Vite + React site on Cloudflare Pages. **It stays static.** This service never serves visitors. It hosts the admin and feeds the site's build:

```
admin saves → owner clicks "Publicar"
   → Cloudflare Pages deploy hook
   → the site build fetches /api/products
   → static site republished (~1 min)
```

The consequence worth internalising: **if this backend is down, asleep or mid-deploy, the shop stays online.** That is why free tiers and a machine that sleeps are acceptable. Do not propose changes that make the storefront depend on this service at request time without flagging that it breaks this property.

---

## 2. Where things stand

### Done and verified

| Area | State |
|---|---|
| HTTP server | `net/http` pattern routing, structured logging, panic recovery, security headers, graceful shutdown on SIGTERM |
| Health checks | `/healthz` (liveness, no DB) and `/readyz` (readiness, DB) |
| Configuration | Env-only, validated, `.env.local` auto-loaded in dev |
| Database | Neon Postgres 18, region `aws-sa-east-1`, connected and migrated |
| Schema | `products`, `product_images`, `site_state` + `updated_at` trigger |
| Domain model | `Product`, `Image`, `View`, `Slugify`, validation |
| Ports | Four interfaces declared, **zero implemented** |
| Architecture guard | Test fails if the core gains a forbidden import |
| Packaging | Dockerfile, `fly.toml`, Makefile, GitHub Actions CI |

Test coverage: `catalog` 91.3%, `adapter/web` 91.5%, `platform/config` 83.3%, `platform/database` 70.0%. `make check` passes with `-race`.

### Not done

Everything that makes it an admin. There are **no screens, no authentication, no persistence adapter**. The `/admin` route returns a placeholder paragraph.

The four ports in `internal/catalog/ports.go` are declared and unimplemented on purpose: they describe the shape of the work ahead. The human has been told this and may yet decide to declare each port only when implementing it — **ask before assuming either way**.

---

## 3. Non-negotiables

Break these and the work gets reverted.

**Hexagonal, dependencies pointing inward.** `internal/catalog` is the core: it imports nothing from this project, no driver, no framework. Adapters depend on the core, never the reverse. Ports live in the core so the core owns the contract.

This is enforced by `internal/catalog/architecture_test.go`, which was validated by planting a violation and watching it fail. If you find it inconvenient, that is the test doing its job — put the dependency behind a port instead of weakening the test.

**Code in English, UI in Portuguese.** Comments, identifiers, logs, errors, docs and commit messages are English. Anything a shop owner reads stays Portuguese: view labels (`Frente`, `Costas`), validation messages (`"Falta o preço"`), the error page. This is not an oversight — translating them would defeat the goal of the owners operating alone.

**Integer cents, never floats.** `29.90 * 3` in floating point is `89.69999999999999`. The admin takes reais and converts at the edge, in one place.

**The admin must be usable by a non-technical person alone.** Plain Portuguese, no jargon ("endereço da peça", not "slug"), validation next to the field, confirmation before deleting, the publish button always visible with its state. This is the acceptance criterion, not a nice-to-have.

---

## 4. The next milestone (M1)

Make products manageable. In rough order:

1. **`internal/adapter/postgres`** implementing `catalog.Repository`. The plan calls for `sqlc` — not installed locally, see §6.
2. **Authentication.** Sessions via `alexedwards/scs` stored in Postgres, argon2id passwords, **no public signup**, users created by `cmd/adduser`. Login rate-limited per IP.
3. **CRUD screens** in `html/template` + HTMX, embedded with `go:embed`. No build step for the admin.
4. **Password reset by email** via Resend. Treat this as required, not optional: if the owners lock themselves out and need the developer, the whole point is lost.

M2 is photos (libvips → WebP at 600w and 1200w → R2). M3 is the publish flow. M4 wires the static site's build to `/api/products`. Full plan: `~/.claude/plans/graceful-gathering-koala.md` (in Portuguese; note that this directory has been wiped by an automated cleanup once already — copy anything you rely on).

---

## 5. Landmines

Each of these cost time already. They are not hypothetical.

**`/healthz` must not touch the database.** Neon's free tier suspends when idle. Fly uses `/healthz` to judge machine health — if liveness depended on Postgres, the machine would be marked unhealthy every time the database napped, and restart in a loop. Readiness is where the DB check belongs.

**Use the direct Neon connection, not the pooled one.** `DATABASE_URL_UNPOOLED`, not `DATABASE_URL`. pgx keeps its own pool; stacking it on PgBouncer's transaction pooling breaks the prepared statement cache. `internal/platform/config` already prefers the right one — do not "fix" it.

**`MinConns = 0` is deliberate.** An idle connection keeps Neon's compute awake and burns free-tier hours while nobody is using the admin.

**`now()` is transaction-start time.** The `updated_at` trigger uses it, which is correct for real use (one save, one transaction) but means a test that updates inside a single transaction will see no change. The existing test uses separate statements for this reason.

**goose needs `-- +goose StatementBegin/End`** around plpgsql function bodies, or it splits them on the inner semicolons.

**HTML `width`/`height` attributes beat CSS `aspect-ratio`.** Bit the sibling repo: thumbnails rendered 1168px tall because `aspect-ratio` is ignored while height resolves to a concrete value. Fix is `height: auto`. Relevant when the admin renders photo previews.

**Verify guards by breaking them.** Both the architecture test and the Makefile's gofmt check were confirmed by planting a failure first. A guard nobody has seen fail is a guard nobody knows works.

---

## 6. What you cannot do from this machine

Check before planning around any of these.

| Missing | Consequence |
|---|---|
| `docker` | The Dockerfile has **never been built or run**. Treat it as reviewed, not verified. |
| `flyctl` | No deploy has happened. The app does not exist on Fly yet. |
| `psql` | Inspect the database through Go tests or the `neon` CLI. |
| `libvips` | M2's image pipeline cannot be tested locally until installed. |
| `sqlc`, `goose` CLI | Both usable as libraries; goose already is. Install the CLI if you want codegen. |
| `staticcheck`, `govulncheck` | CI runs them; locally `make check` covers gofmt, vet and tests only. |

**You cannot `git push`.** The SSH key is passphrase-protected and no `ssh-agent` is running, so ssh offers the key and cannot decrypt it. Commit locally and let the human push, unless they start an agent with the key loaded.

**Two `neon` CLI commands are blocked** by the sandbox classifier: `npx neon skills` (installs remote agent instructions) and `neon deploy` (flagged as traffic redirection). Neither was needed — the database is provisioned and credentials are in `.env.local`. If you genuinely need them, ask the human rather than working around it.

Toolchain: Go 1.27.2 via asdf, pinned in `.tool-versions`. Node 22 via nvm at `~/.nvm/versions/node/v22.23.2/bin` (needed only for the `neon` CLI and the sibling site).

---

## 7. How to verify your work

```bash
make check                               # gofmt, vet, tests with -race
go test ./internal/catalog/ -run TestCore # the architecture guard specifically
go test ./internal/platform/database/ -v  # integration; skips without a database
make run                                  # then curl /healthz and /readyz
```

Integration tests skip silently when `DATABASE_URL` is absent, so **confirm they actually ran** rather than trusting a green suite.

The honest end-to-end test for M1 is not a passing suite: it is one of the shop owners creating a product without asking for help. Until that works, M1 is not done.

---

## 8. Open questions for the human

- Should the four unimplemented ports stay declared, or be introduced one at a time as each adapter lands?
- `sqlc` codegen, or hand-written `pgx` queries? The plan says sqlc; the CLI is not installed.
- The Fly app has never been deployed. Who creates it, and when?
- `bfc6058` is committed locally but **not pushed** — the human wanted to review it first. Confirm it landed before building on top of it.
