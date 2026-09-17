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
2. [ ] Postgres schema + migrations.
3. [ ] Domain layer (entities, `Money`, `Transaction` aggregate root,
   repository interfaces).
4. [ ] Core ledger use cases (Deposit/Withdraw/Transfer) end to end.
5. [ ] Idempotency.
6. [ ] Mock payment provider — async webhooks, configurable misbehavior.
7. [ ] Event sourcing + replay.
8. [ ] Reconciliation + pending/settled state machine.
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
