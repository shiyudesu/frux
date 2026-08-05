## Context

Frux has logs and domain events but no durable record dedicated to privileged operator actions. Future review, enforcement, configuration, and replay operations must not commit state without attributable audit evidence. The application remains a monolith over one PostgreSQL database, so the first design can use the same database transaction as the protected mutation.

## Goals / Non-Goals

**Goals:**

- Store immutable, privacy-bounded audit facts for privileged attempts and committed mutations.
- Guarantee that a successful durable mutation and its success audit record commit together.
- Provide stable cursor pagination for authorized audit readers.
- Make the contract reusable without making the audit module own other domains.

**Non-Goals:**

- Event sourcing, general application logging, SIEM export, user activity history, or audit-row editing.
- Storing full request or response bodies.
- Retrofitting historical admin actions.

## Decisions

### Use one append-only `admin_audit_event` table

Each event has a generated ID, actor ID, permission, action, target type and ID, outcome, request ID, idempotency key when present, bounded JSON detail, and creation time. No update or delete repository method is exposed.

### Make successful audit insertion part of the owning mutation

Privileged repository methods accept a validated audit fact and insert it through a shared infrastructure helper inside the same GORM transaction as the domain change. The audit application package owns validation and query behavior; it does not coordinate arbitrary cross-domain transactions.

Alternative: write audit asynchronously through RabbitMQ. Rejected because a successful mutation could become permanently unaudited.

### Record rejected attempts separately from durable mutations

Authorization and validation failures may be recorded best-effort after the response decision because no business state commits. Failure to record a denied attempt is observable but does not replace the original error.

### Keep details bounded and allow-listed

Detail is limited in size and depth and may contain reason codes, prior/new version numbers, or queue names. Tokens, passwords, raw media, full payloads, and arbitrary headers are rejected or redacted before persistence.

### Query with `(created_at, id)` cursor ordering

Readers may filter by actor, action, target type, outcome, and bounded time range. Results order by `created_at DESC, id DESC`.

## Risks / Trade-offs

- [Every privileged mutation must remember the audit fact] -> Make it a required repository input and cover omission with integration tests.
- [JSON details can leak sensitive data] -> Validate allow-listed keys per action and impose a small serialized limit.
- [Audit storage grows indefinitely] -> Add indexes now and leave retention/export to a separate reviewed change.

## Migration Plan

1. Add the table, entity validation, repository, and query API.
2. Integrate the first consuming admin use case only after its state change can insert audit in the same transaction.
3. Add metrics for successful, rejected, and failed audit writes.
4. Roll back consumers before removing the query route; keep committed audit rows intact.

## Open Questions

- Retention and external immutable export requirements after real operator volume is known.
