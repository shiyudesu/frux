## 1. Account Domain Contracts

- [x] 1.1 Add canonical normal, frozen, and cancelled account status constants and validation helpers without changing existing registration defaults.
- [x] 1.2 Define ordinary-account list/detail projections, stable query/filter types, cursor tuple, management operation names, result snapshots, and registered reason codes.
- [x] 1.3 Define freeze, unfreeze, and force-sign-out command validation including positive IDs, expected version, role boundary, allowed state transitions, and 128-character idempotency-key rules.
- [x] 1.4 Add domain errors for invalid queries, cursors, reasons, transitions, stale versions, ordinary-user absence, and idempotency conflicts.
- [x] 1.5 Add narrow account-management repository interfaces for list, detail, and transactional operation commit.
- [x] 1.6 Add domain tests covering status transitions, cancelled-account rejection, ordinary-role enforcement, reason registries, fingerprint stability, result restoration, and validation boundaries.

## 2. Authorization and Audit Contracts

- [x] 2.1 Register `account.manage` in the closed admin permission registry and grant it only through the compatibility `admin` role.
- [x] 2.2 Extend authorization tests to prove admin access and reviewer, operator, user, unknown-role, inactive-account, stale-auth-version, and wrong-token rejection.
- [x] 2.3 Register `account.freeze`, `account.unfreeze`, and `account.sessions_revoke` audit actions plus the `user_account` target type.
- [x] 2.4 Bind each account audit action to `account.manage`, its exact route/method, status/version fields, bounded revoked-session count, and operation-specific reason codes.
- [x] 2.5 Add audit-domain tests for valid facts, contradictory transitions, unsupported detail keys, sensitive detail rejection, idempotency-key hashing, and denied-attempt schemas.

## 3. PostgreSQL Persistence and Migration

- [x] 3.1 Add the account-management idempotency model with actor-scoped unique key, request fingerprint, target, operation, serialized result, and creation time.
- [x] 3.2 Register the new model in the shared migration transaction and add the `(role, status, created_at DESC, id DESC)` account-list index with an explicit table-prefixed name.
- [x] 3.3 Implement ordinary-account list persistence with `role='user'`, escaped account/nickname filtering, status and ID filters, stable tuple pagination, bounded limit, and set-based aggregate hydration.
- [x] 3.4 Implement ordinary-account detail persistence with aggregate relationship/content statistics and active refresh-session count while selecting no password or session credential columns.
- [x] 3.5 Implement the locked transactional freeze path with expected-version comparison, `role='user'` recheck, status/auth-version update, active refresh-session revocation, audit append, idempotency result, and committed-write metrics.
- [x] 3.6 Implement unfreeze and force-sign-out in the same transaction boundary, preserving status for force-sign-out and never restoring revoked sessions.
- [x] 3.7 Implement same-key replay and different-fingerprint conflict handling without duplicating mutations or success audit facts.
- [x] 3.8 Add repository unit/SQL-shape tests proving private credential columns are excluded, filters are escaped, sort order is stable, and privileged roles are always filtered.
- [x] 3.9 Add PostgreSQL integration tests for pagination, detail aggregates, concurrent version conflicts, concurrent idempotent replay, role changes between read and mutation, cancelled accounts, zero-session revocation, and status preservation.
- [x] 3.10 Add transaction-failure tests proving account, auth version, refresh sessions, idempotency result, and audit all roll back when any protected write fails.

## 4. Account Management Application Service

- [x] 4.1 Implement request normalization, default/max limits, filter hashing, HMAC cursor encoding/decoding, and filter-bound cursor rejection.
- [x] 4.2 Implement ordinary-account list and detail use cases with repository-error wrapping that preserves domain validation and absence errors.
- [x] 4.3 Implement freeze, unfreeze, and force-sign-out orchestration, including audit fact construction, request IDs, idempotency-key digesting, and truthful result/replay responses.
- [x] 4.4 Add application tests for cursor round trips, filter mismatch, default limits, stable next cursors, unavailable repositories, registered reasons, and exact audit fact contents.
- [x] 4.5 Add application tests for stale versions, invalid transitions, non-user targets, idempotent replay, idempotency conflicts, audit build failures, and bounded residual-access semantics.

## 5. HTTP API and Composition

- [x] 5.1 Add account-management DTOs and handlers for list, detail, freeze, unfreeze, and session revocation while keeping parsing and error mapping out of the application service.
- [x] 5.2 Parse positive IDs, status/query/limit/cursor filters, required `Idempotency-Key`, expected version, and registered reason codes with stable account-management error codes.
- [x] 5.3 Register `/api/admin/accounts`, `/api/admin/accounts/:userId`, `/freeze`, `/unfreeze`, and `/sessions/revoke` routes behind admin authentication and `account.manage`.
- [x] 5.4 Wire the account-management service, repository, HMAC cursor secret, admin audit repository, and denied-attempt audit configuration in the router composition root.
- [x] 5.5 Add API-flow tests for list/detail privacy, status filters, pagination, admin success, operator/reviewer 403, consumer-token 401, privileged target exclusion, invalid requests, conflicts, replay, and unavailable dependencies.
- [x] 5.6 Verify freeze and force-sign-out reject subsequent refresh, unfreeze requires a new password login, and an already issued short access token follows the documented bounded-expiry behavior.

## 6. Admin Web Experience

- [x] 6.1 Extend `AdminPermission`, runtime guards, account DTOs, and API client functions for `account.manage` and all account-management endpoints without using `any`.
- [x] 6.2 Add `/admin/accounts` to the typed route unions, normalization, login return validation, navigation targets, and router tests.
- [x] 6.3 Replace the Admin Shell route-permission conditional with an explicit mapping and add the “账号管理” destination only for server-confirmed `account.manage`.
- [x] 6.4 Build the ordinary-account filter toolbar and stable load-more table with account, nickname, status, registration time, aggregate summaries, loading, empty, error, and retry states.
- [x] 6.5 Build the bounded account detail view showing permitted profile/stat fields, active-session count, version, and a link into existing video operations without exposing privileged accounts.
- [x] 6.6 Add freeze, unfreeze, and force-sign-out confirmation dialogs with registered reason selectors, generated idempotency keys, expected-version submission, disabled pending state, and clear content-separation/residual-token copy.
- [x] 6.7 Refresh affected list/detail data after success or replay, preserve filters and pagination where safe, and reload on stale-version conflicts without reporting a false success.
- [x] 6.8 Clear account-management data on authoritative 403, preserve admin-session-only 401 behavior, and render unavailable responses without displaying raw backend errors.
- [x] 6.9 Add Web tests for permission-filtered navigation, direct-route denial, list/filter/load-more behavior, privacy-bounded rendering, each mutation, replay, conflict refresh, 401/403 cleanup, empty states, and error retries.

## 7. Documentation

- [x] 7.1 Update account, admin, and admin-audit module documentation with ordinary-user-only scope, APIs, statuses, permission, audit actions, idempotency, session effects, and content separation.
- [x] 7.2 Update product, architecture, engineering, UI/UX, and API error documentation with the new route, data flow, transactional boundary, UX states, and bounded access-token limitation.
- [x] 7.3 After implementation validation, mark item 44 in `docs/当前问题.md` resolved with an accurate concise description that explicitly excludes administrator-account management.

## 8. Validation

- [x] 8.1 Run gofmt and targeted domain, application, persistence, middleware, migration, admin-audit, account-session, and API-flow Go tests.
- [x] 8.2 Run `cd apps/api && go build ./cmd/feed ./cmd/worker` and expand to `go test ./...` if targeted results expose shared regressions.
- [x] 8.3 Run `pnpm -C apps/web run test`, `pnpm -C apps/web run lint`, and `pnpm -C apps/web run build`.
- [x] 8.4 Run `openspec validate --all --strict` and reconcile the change artifacts and affected documentation with the final verified behavior.

## 9. Frozen Login and Notification Domain

- [x] 9.1 Add a dedicated frozen-account login result while preserving dummy bcrypt and generic invalid credentials for unknown accounts, wrong passwords, and cancelled accounts.
- [x] 9.2 Define account freeze/unfreeze notification event, outbox state, stable event ID, registered operation/reason validation, lease data, retry state, and terminal errors.
- [x] 9.3 Add domain and application tests proving frozen status is revealed only after password proof and notification payloads cannot carry arbitrary text or unsupported reasons.

## 10. Transactional Notification Outbox

- [x] 10.1 Add and migrate `account_notification_outbox` with stable event uniqueness, recipient, operation, reason, occurrence time, delivery state, attempts, availability, lease, error, and delivered time.
- [x] 10.2 Append freeze and unfreeze outbox rows in the existing account-management transaction while keeping force-sign-out notification-free.
- [x] 10.3 Ensure stale versions, audit failure, idempotency conflicts, and transaction failures leave no outbox row, while same-key replay creates no duplicate row.
- [x] 10.4 Add PostgreSQL tests for atomic account/audit/outbox commit, rollback, stable event identity, concurrent replay, claim ordering, lease fencing, retry, and terminal completion.

## 11. Account Lifecycle Message Delivery

- [x] 11.1 Add a narrow `AccountLifecycleMessageWriter` and message Application method that maps registered freeze/unfreeze reasons to bounded Chinese `SYSTEM` titles and content.
- [x] 11.2 Implement the supervised account notification Worker with bounded batches, `FOR UPDATE SKIP LOCKED`, stable lease owner, timeout, exponential backoff, and terminal classification.
- [x] 11.3 Wire the account outbox repository, message writer adapter, Worker metrics/logging, startup, and supervised shutdown in `cmd/worker`.
- [x] 11.4 Add tests for transient retry, terminal payloads, Worker restart/redelivery, stable message idempotency, one message per lifecycle event, and force-sign-out suppression.

## 12. Login and Web Experience

- [x] 12.1 Map correct-password frozen login to HTTP 423 `AUTH_ACCOUNT_FROZEN` without setting Access, Refresh, or asset cookies; retain existing generic responses for unknown/wrong/cancelled credentials.
- [x] 12.2 Add safe Web error copy for frozen login and tests proving the login page distinguishes it from invalid credentials only when the backend returns the dedicated code.
- [x] 12.3 Add API-flow coverage showing a residual short Access Token can read a delivered freeze message, and a user can read retained freeze/unfreeze messages after unfreeze and a new login.

## 13. Documentation and Revalidation

- [x] 13.1 Update account, message, admin, architecture, engineering, product, UI/UX, error, and current-issues documentation with password-proven frozen login and durable freeze/unfreeze notifications.
- [x] 13.2 Run targeted and full Go/Web tests and builds, lint, PostgreSQL integration tests where configured, and `openspec validate --all --strict`.
