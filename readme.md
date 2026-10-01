# Ledger / Payment Reconciliation Engine

A double-entry ledger with event-sourced history, reconciled against a mock
payment provider's independent record of the same transactions. Built as a
learning project — my first time using Domain-Driven Design and my first
time using Postgres (previous projects were MySQL/layered-architecture, see
`identity-service` and `casino-service`).

## What this is

Two pieces, unequal in weight:

1. **The ledger/reconciliation engine** — the real project. Accounts,
   balanced debit/credit entries, idempotent processing, an event-sourced
   history, and a reconciliation engine that compares internal state against
   an external provider's record of the same transactions.
2. **A mock payment provider** — a small, deliberately lightweight service
   simulating M-Pesa/Daraja-style webhooks (async delivery, configurable
   failures: duplicate delivery, missing delivery, amount mismatches). Exists
   purely to give the reconciliation engine something real to reconcile
   against. No DDD treatment — it's a test harness, not a second project.

## Architecture

Domain-oriented folders, not layer-oriented — a deliberate departure from
how I've structured Go services before:

```
/cmd
  /server        # the ledger app entrypoint
  /mockprovider  # the mock payment provider entrypoint
/internal
  /ledger            # entities, Money value object, Transaction aggregate root, repository interface, use cases
  /reconciliation     # entity, service comparing internal ledger vs provider statement
  /postgres            # implements the repository interfaces — no business logic
  /http                # translates HTTP <-> service calls — no business logic
/migrations
```

Key decisions:
- `Transaction` is the **aggregate root** for the ledger domain — `Entry`
  rows are only ever created through it (e.g. `NewTransfer(...)`), which is
  what makes "entries always sum to zero" structurally guaranteed instead of
  just hoped for.
- `Money` (amount in minor units + currency) is a **value object**, not its
  own table.
- Deliberately *not* using CQRS, a generic event-bus abstraction, or full
  four-ring Clean Architecture — the domain/postgres/http split is the
  actual payoff here.

Six tables, two bounded contexts:

**Ledger** — `accounts`, `transactions` (aggregate root), `entries`
(immutable), `events` (append-only domain event log, the real source of
truth for event sourcing).

**Reconciliation** — `provider_statement_lines`, `discrepancies`.

## Coding conventions

Structure is DDD; the code itself follows the house style from prior
projects (`identity-service`, `casino-service`):
- `logrus` with a JSON formatter, chained as
  `logrus.WithContext(ctx).WithError(err).WithFields(logrus.Fields{...}).Error(err.Error())`.
- Env vars read directly at point of use via `os.Getenv`, not funneled
  through a central config struct.
- Exported structs with a doc comment per field, JSON tags in snake_case.
- Shared dependencies (DB pool, etc.) held on one struct, injected once.

Not carried over from those projects: the OpenTelemetry/Uptrace tracing and
metrics boilerplate wrapped around every method. Observability here is
scheduled for its own build phase, reusing the metrics/tracing/Grafana stack
from the rate-limiter project instead.

## Local development

```
go mod tidy
docker compose up -d --build
docker compose ps
curl http://127.0.0.1:8081/health   # app
curl http://127.0.0.1:8082/health   # mock provider
```

Two env files: `.env` for running the app as a plain process (`go run
./cmd/server`), `.env.docker` for the containerized stack (gitignored, never
committed — created manually on the VPS).

## Deployment

Own hardened Contabo VPS (Ubuntu, Docker + Compose, Nginx on the host
handling all projects, Certbot per subdomain) — shared with an existing
project (`clinic-booking`). Ports:

- App: `127.0.0.1:8081`
- Postgres: `127.0.0.1:5433`
- Mock payment provider: `127.0.0.1:8082`
- Subdomain: `ledger.ryanwanjie.com`

Every container port is bound to `127.0.0.1` explicitly — Docker inserts
`iptables` rules ahead of UFW's, so an unbound port is reachable from the
public internet regardless of what UFW reports.

## Build phases

1. [x] Docker infrastructure — Postgres + app + mock-provider containers,
   healthchecks, no domain code.
2. [x] Postgres schema + migrations.
3. [x] Domain layer (entities, `Money`, `Transaction` aggregate root,
   repository interfaces).
4. [x] Core ledger use cases (Deposit/Withdraw/Transfer) end to end.
5. [x] Idempotency.
6. [x] Mock payment provider — async webhooks, configurable misbehavior.
7. [x] Event sourcing + replay.
8. [x] Reconciliation + pending/settled state machine.
9. [ ] Full observability + VPS deploy.

## Build log

### Phase 1 — Docker infrastructure (2026-09-08 → 2026-09-09)

Compose stack (Postgres 16 + app + mock provider), multi-stage Dockerfiles
(`golang:1.25-bookworm` → `debian:12-slim`, non-root `appuser`), Postgres
healthcheck gating `depends_on`, two-env-file pattern. Both services expose
a `/health` endpoint; the app's also pings the DB, so a 200 is proof the
whole chain — container, network, Postgres, connection pool — actually
works, not just that the process started.

**Incident: containers stuck in a restart loop on the VPS.** Both
`cmd/server/main.go` and `cmd/mockprovider/main.go` had their package
declared as `package server` / `package mockprovider` instead of
`package main`. Go only produces a runnable binary from a `main` package —
anything else builds fine as a library, so the Docker image itself built
without error, but there was nothing valid for `ENTRYPOINT` to actually
run. The container exited immediately, Compose's restart policy relaunched
it, and it looked from the outside like a crash loop with no obvious cause.
About two hours of debugging before catching it — the fix was renaming both
package declarations to `package main`. Worth remembering: when a container
restart-loops right after a build (not after running for a while), check
that the entrypoint binary is actually the thing you think it is before
chasing runtime theories.

Verified on the VPS:
```
curl http://127.0.0.1:8081/health   # {"status":"ok"}
curl http://127.0.0.1:8082/health   # {"service":"mock-payment-provider","status":"ok"}
```

### Phase 2 — Postgres schema + migrations (2026-09-21)

Six tables (`accounts`, `transactions`, `entries`, `events`,
`provider_statement_lines`, `discrepancies`) plus a `transaction_status`
ENUM, `gen_random_uuid()` for PKs, `JSONB` for the event payload,
`TIMESTAMPTZ` throughout. Migrations run via `golang-migrate`
(`pgx/v5` driver, avoids pulling in `lib/pq` as a second Postgres driver
family) embedded in the same binary as the server, gated by a `SETUP_TYPE`
env var — same pattern as `identity-service`. `docker-compose.yml` runs it
as a one-shot `migrate` service (`SETUP_TYPE=cronjob`, `restart: "no"`)
that `app` waits on via `depends_on: condition: service_completed_successfully`,
so migrations always apply before the server starts, with no manual step
and no separate migration CLI in the image.

**Two bugs caught in review before this ever touched the VPS:**

1. `cmd/server/main.go` was missing two blank imports —
   `_ "github.com/jackc/pgx/v5/stdlib"` (registers the `"pgx"` driver
   `database/sql` needs for `sql.Open`) and
   `_ "github.com/golang-migrate/migrate/v4/source/file"` (registers the
   `file://` source golang-migrate needs to read `.sql` files off disk).
   Both are the kind of import that compiles fine and only fails at
   runtime — `sql: unknown driver "pgx"` — so it wouldn't have shown up
   until the migration actually tried to run.
2. The Dockerfile's `COPY migrations /migrations` referenced a folder
   named `migrations` (plural); the one on disk was `migration`
   (singular). Would have failed the image build outright with
   `COPY failed: file not found`. Fixed by renaming the folder to match
   the convention the Dockerfile and `file:///migrations` URL both
   already assumed.

Verified on the VPS:
```
$ docker compose ps
app            Up (healthy dependency chain: postgres → migrate → app)
migrate        Exited (0)
postgres       Up (healthy)

$ psql -U ledger_app -d ledger -c '\dt'
accounts | discrepancies | entries | events | provider_statement_lines
| schema_migrations | transactions   (7 rows)

$ psql -U ledger_app -d ledger -c 'select * from schema_migrations;'
 version | dirty
---------+-------
       1 | f
```

### Phase 3 — Domain layer (2026-09-26)

Pure Go in `internal/ledger/`, no framework or database imports:
`Money` (value object, minor units + currency), `Account` (entity),
`Entry` (immutable, unexported constructor), `Transaction` (aggregate
root), and the repository *interfaces* (implemented later by
`internal/postgres`).

Deliberate departures from the house style, because DDD needs them:
- Domain types use **unexported fields + getter methods**, not exported
  fields with JSON tags. That is what makes "entries always sum to zero"
  a compile-time guarantee: no package outside `ledger` can build an
  `Entry` or mutate a `Transaction`'s entries. Exported JSON structs
  return at the HTTP boundary, as DTOs.
- **No logging inside the domain types.** Logging starts at the
  service and repository layers, where operations actually happen.
- Tests are deliberately deferred until the project is complete.

### Phase 4 — Core ledger use cases (2026-09-29)

`LedgerService` (`internal/ledger/service.go`) wraps the domain
constructors with persistence: `CreateAccount`, `GetAccount`,
`Deposit`/`Withdraw`/`Transfer`. `internal/postgres` implements
`AccountRepository`/`TransactionRepository` against the real schema —
`TransactionRepository.Save` writes a transaction and all of its entries
inside one DB transaction, so a half-written ledger entry can never exist.
`internal/http/handler.go` is the Echo adapter translating requests to
service calls and back; no business logic lives there.

Deposits and withdrawals need two sides to balance, so migration 2 seeds a
fixed `EXTERNAL` account — deposits debit it, withdrawals credit it,
transfers can never touch it (enforced in the domain, not just by
convention).

**Bug found after deploying: no overdraft protection.** A transfer or
withdrawal succeeded even from a zero balance. Fixed with a row lock inside
`TransactionRepository.Save` — `SELECT ... FOR UPDATE` on the debited
account, then the balance re-derived with the same `SUM` query
`GetBalance` uses (as a second statement; Postgres rejects combining
`FOR UPDATE` with an aggregate in one query), with `SET LOCAL
lock_timeout = '2s'` so a request can't wait forever behind another. Proven
under load: 20 concurrent withdrawals of 100 against a balance of 1000 —
exactly 10 succeeded, balance never went negative.

### Phase 5 — Idempotency (2026-09-29)

Retrying the same request with the same `Idempotency-Key` now returns the
original result (`200`) instead of creating a second transaction (`201`
the first time). The `idempotency_key` unique constraint from Phase 2 is
the entire concurrency mechanism — no app-level locking needed: two
concurrent identical requests both try to `INSERT`, Postgres serializes
them and the loser gets a `23505` conflict, which `Save` catches and turns
into "go fetch what the winner wrote."

**Bug caught in testing, not asked for:** the overdraft check originally
ran *before* the insert, so retrying an already-successful withdrawal
after the account's balance had since moved (other activity happened on
it) could come back `422 insufficient funds` instead of replaying the
original result. Fixed by moving the `INSERT INTO transactions` first —
if it hits the duplicate-key conflict, return before ever touching the
balance; only a genuinely new transaction reaches the overdraft check.

### Phase 6 — Mock payment provider webhooks (2026-10-02)

New bounded context `internal/reconciliation/` (entity, repository
interfaces, service) — thin for now, just enough to give an incoming
provider statement a durable, idempotent home. New outbound port
`ledger.PaymentProviderClient`, implemented over HTTP by
`internal/providerclient` (domain stays framework-free; the interface
lives in `ledger`, same dependency-inversion pattern as the repositories).

`cmd/mockprovider` gained `POST /charges` (accepts a reference, amount, and
an optional `behavior`) plus async webhook delivery after a short random
delay, simulating four outcomes: `normal`, `duplicate` (delivers twice),
`missing` (never delivers), `mismatch` (reports a different amount),
`declined` (reports status `failed`). `Deposit()` calls the provider
after saving the transaction as `pending` — best-effort: if the provider
is unreachable, the transaction still exists and stays pending rather than
the whole API call failing.

Duplicate webhook delivery is handled the same way duplicate idempotency
keys are — a Postgres `ON CONFLICT (provider_reference) DO NOTHING`, not
app-level dedup logic.

### Phase 7 — Event sourcing + replay (2026-10-02)

Every `TransactionRepository.Save` now also appends a `transaction.posted`
event (same DB transaction as the entries, so the log can't drift from
what was actually written). `GET /admin/replay` rebuilds every account's
balance purely from the event log and compares it against the live
balance (summed from `entries`) — proof the log alone is sufficient to
reconstruct current state.

**Bug caught by the replay check itself:** when Phase 8's reconciliation
marks a transaction `failed`, that status change wasn't recorded as an
event — only the original `pending` status was ever in the log. Replay
had no way to know the transaction later failed, so it kept counting
entries `GetBalance` correctly excludes (`status <> 'failed'`), and
`all_match` came back `false`. Fixed by making status transitions their
own events (`transaction.settled` / `transaction.failed`, written
atomically with the status `UPDATE`) and replaying in two passes: first
reconstruct each transaction's latest status from the full event stream,
then sum entries only for transactions not left `failed` — the same rule
`GetBalance` applies, now derived purely from events instead of asked of
the live table.

### Phase 8 — Reconciliation + state machine (2026-10-02)

`reconciliation.Service.Reconcile` (triggered via `POST /admin/reconcile`)
compares every pending deposit older than a grace period
(`RECONCILIATION_GRACE_PERIOD`, default 30s) against what the provider
reported for the same reference: matching amount + `settled` → the
transaction is marked settled; matching amount + any other status →
marked failed; wrong amount → flagged as an `amount_mismatch`
discrepancy, left pending for a human; no statement at all yet → flagged
as `missing_statement`, also left pending. Discrepancy inserts are
conditional (`WHERE NOT EXISTS ...`) so re-running `Reconcile` never piles
up duplicate unresolved rows for the same already-flagged problem.

Known, deliberate gaps: only deposits are reconciled (withdrawals were
never wired to the provider in Phase 6); a statement line matching no
transaction at all ("orphan" webhook) isn't checked — only the
ledger→provider direction is, not the reverse.
