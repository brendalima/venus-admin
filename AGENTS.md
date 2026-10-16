# Working rules — Vênus Oasis Admin

Durable conventions for this repository. For current state and the next
milestone, read [docs/HANDOFF.md](docs/HANDOFF.md) instead.

---

## The invariant

The public storefront (`../venus-site`) is **static** and must stay that
way. This service feeds its build; it does not serve visitors.

The property that buys: if this backend is down, asleep or mid-deploy,
the shop stays online. That is what makes free tiers and a sleeping
machine acceptable.

Any change that makes the storefront depend on this service at request
time breaks that. It may still be the right call one day — but say so
out loud rather than letting it happen as a side effect.

## Architecture: hexagonal, dependencies inward

`internal/catalog` is the core. It imports **nothing** from this project,
no driver and no framework. Adapters depend on the core, never the
reverse.

Ports live in the core, so the core owns the contract. That also matches
Go's own habit of declaring interfaces where they are consumed.

```
cmd/server/        composition root — the only place that wires things
internal/catalog/  the core: domain, rules, ports
internal/adapter/  driving and driven adapters
internal/platform/ configuration, database connection
```

`internal/catalog/architecture_test.go` enforces this. When it blocks
you, that is the test working: put the dependency behind a port rather
than weakening the test.

## Language

**Code is English** — comments, identifiers, logs, errors, docs, commit
messages.

**Anything a shop owner reads is Portuguese** — view labels (`Frente`,
`Costas`), validation messages (`"Falta o preço"`), the error page.

This split is deliberate. The owners run this admin without the
developer; translating their side would defeat the point.

Note the sibling repo `../venus-site` is **Portuguese throughout**,
including code comments. Do not "harmonise" the two.

## Money

Integer cents, never floats. `29.90 * 3` in floating point yields
`89.69999999999999`. Input arrives in reais and converts at the edge, in
one place.

## Database

Postgres on Neon, region `aws-sa-east-1`, same region as the app.

- Use **`DATABASE_URL_UNPOOLED`**, the direct connection. pgx keeps its
  own pool; stacking it on PgBouncer's transaction pooling breaks the
  prepared statement cache. `internal/platform/config` already picks the
  right one.
- **`MinConns = 0`** is deliberate: an idle connection keeps Neon's
  compute awake and burns free-tier hours.
- **`/healthz` must not touch the database.** Neon sleeps; Fly judges
  machine health from that endpoint. Readiness is where the DB check
  belongs.
- Migrations are embedded with `go:embed` and run at startup. Safe with
  one machine; with more, move it to a `release_command`.

## The admin is for non-technical users

This is an acceptance criterion, not a nice-to-have. Plain Portuguese, no
jargon ("endereço da peça", not "slug"), validation next to the field,
confirmation before deleting, publish button always visible with its
state.

The real test of a feature is one of the owners using it without asking
for help.

## Verifying

- `make check` before claiming anything works. It runs gofmt, vet and
  tests with `-race`.
- **Break your own guards once.** The architecture test and the gofmt
  check were both confirmed by planting a failure first. A guard nobody
  has seen fail is a guard nobody knows works.
- Integration tests **skip silently** without `DATABASE_URL`. Confirm
  they ran; a green suite that skipped everything proves nothing.
- Do not claim something is verified when it was only reviewed. The
  Dockerfile, for instance, has never been built — there is no Docker on
  the development machine.

## Git

**Do not push.** The SSH key is passphrase-protected with no agent
running, so an agent cannot authenticate. Commit locally and let the
human push.

Commit when a piece of work is complete, not mid-change. Never rewrite
history that has been pushed.

Commit messages: English, imperative subject, and a body that says *why*
when the reason is not obvious from the diff.
