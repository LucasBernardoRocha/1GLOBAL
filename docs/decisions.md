# Design decisions

This document records the main trade-offs behind the implementation. The goal
was a small, production-shaped service rather than a feature-complete platform.

## ADR-001: PostgreSQL for persistence

**Decision:** use PostgreSQL through `pgx` and run the service with a dedicated
database container.

**Why:**

- The challenge requires durable persistence and explicitly excludes in-memory
  storage.
- Device rules are invariants that span a read and a write: an `in-use` device
  must not be deleted, and its name/brand must not change. PostgreSQL provides
  ACID transactions and `SELECT ... FOR UPDATE`, so the check and the write are
  atomic under concurrent requests.
- The domain is close to a payments platform: transaction consistency,
  traceability and reconciliation matter. PostgreSQL is a better fit than a
  single-writer embedded database and leaves room for an outbox table,
  idempotency keys and audit records.
- It supports multiple API replicas without sharing a local file.

**Trade-offs:** more moving parts than SQLite (container, migrations,
connection configuration, test database). This is mitigated by Docker Compose,
embedded migrations and integration tests that run against a real PostgreSQL.

## ADR-002: Server-Sent Events for state notifications

**Decision:** expose `GET /v1/devices/events` as an SSE stream.

**Why:**

- The notification is unidirectional (server to subscribed clients), which is
  exactly SSE's use case.
- It is HTTP-native, needs no extra protocol or client library, and is easy to
  test with `curl -N` or `httptest`.
- It works with standard proxies and load balancers, subject to buffering
  configuration (`X-Accel-Buffering: no` is set).

**Alternatives:**

- **WebSocket** adds bidirectional framing and connection management that the
  challenge does not need.
- **Webhooks** are the right choice for external consumers (PSPs typically use
  them), but they require a subscriber registry, retries, signatures and
  idempotency. That would be a separate feature.

**Trade-offs:** the broker is in-process, so events are lost on restart and not
shared between replicas. The challenge explicitly allows missed notifications;
a production implementation would use a transactional outbox plus a durable
broker (Kafka/RabbitMQ) or signed webhooks.

## ADR-003: PATCH for full and partial updates

**Decision:** expose only `PATCH /v1/devices/{id}` for updates.

**Why:** pointer fields distinguish omitted values from zero values, so one
endpoint covers both partial and full updates without duplicating validation,
routing and tests. A full update is simply a PATCH containing every mutable
field.

**Trade-off:** clients that expect `PUT` semantics must use PATCH. The OpenAPI
spec documents the behavior.

## ADR-004: No authentication in this challenge

**Decision:** the API is unauthenticated.

**Why:** authentication is not part of the challenge, and adding OAuth2, mTLS
or API-key management would distract from the domain rules and the delivery
timeline.

**Risk:** every endpoint is open to anyone who can reach the service. In a
payments environment this is not acceptable.

**Production path:** terminate TLS at the gateway, require OAuth2 client
credentials or mTLS between services, add API keys for external consumers,
enforce rate limits, record an audit trail of mutations and apply least
privilege at the database level.

## ADR-005: No pagination in this challenge

**Decision:** `GET /v1/devices` returns all matching devices.

**Why:** the challenge asks to fetch all devices, and pagination would add
contract and test complexity without changing the core evaluation.

**Risk:** a large table produces a large response and unnecessary database and
network work.

**Production path:** cursor pagination ordered by `(created_at, id)`, a maximum
page size, a documented default, and indexes that support the selected filters.
The repository already orders by `created_at DESC, id DESC` to make this
transition straightforward.

## ADR-006: In-process broker, no replay

**Decision:** use a small in-memory broker with buffered subscriber channels.

**Why:** it keeps the notification path simple and non-blocking. Slow consumers
are dropped rather than stalling the update request.

**Trade-offs:** no durability, no replay, no cross-replica fan-out. The service
publishes only after the database commit, which satisfies the challenge rule
that notifications cannot precede persistence. A production system should use a
transactional outbox and a durable broker, or webhooks with retries and
idempotency.

## ADR-007: Standard library HTTP

**Decision:** use `net/http` and the Go 1.22+ `ServeMux` patterns instead of a
framework.

**Why:** the routing needs are small, the standard library is well understood,
and avoiding a framework keeps the dependency surface and cognitive load low.
Middleware for request IDs, structured logging and panic recovery is explicit
and easy to review.

## ADR-008: Docker-only development and delivery

**Decision:** all Go commands, tests and runtime processes execute in
containers. The repository does not require a local Go installation.

**Why:** it keeps the developer environment reproducible and matches how the
service is delivered. `scripts/go.sh` wraps the toolchain container, and
`docker compose` runs the API with PostgreSQL.

**Trade-offs:** the first build downloads the Go image and modules, and Docker
must be available. The Make targets hide most of that friction.

## ADR-009: Test strategy

**Decision:** unit tests for the domain and service, HTTP tests with
`httptest`, broker tests, and optional integration tests against a real
PostgreSQL.

**Why:** most rules are pure and fast to test; the HTTP layer is tested through
its public contract; the repository is tested against the same engine used in
production. Integration tests skip when `TEST_DATABASE_URL` is not set, so
`go test ./...` remains usable without Docker.

## Known limitations and next steps

- Add authentication, authorization and rate limiting.
- Add cursor pagination and a maximum page size.
- Add a transactional outbox and a durable event broker or signed webhooks.
- Add Prometheus metrics, OpenTelemetry traces and dashboards.
- Add load tests for list/filter and concurrent state transitions.
- Add request idempotency keys for mutating endpoints.
- Add Kubernetes manifests, PodDisruptionBudgets and a migration job.
- Add TLS termination, secret management and encrypted backups.
- Add SLOs, alerts and runbooks for the production owner.
