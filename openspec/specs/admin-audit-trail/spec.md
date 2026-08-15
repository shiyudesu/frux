# admin-audit-trail Specification

## Purpose
Define durable, privacy-bounded audit evidence and stable query behavior for privileged Frux actions.

## Requirements

### Requirement: Immutable Privileged Action Facts
Frux SHALL persist privileged action facts as append-only audit records containing actor, permission, action, target, outcome, request correlation, bounded detail, and creation time.

#### Scenario: Privileged mutation succeeds
- **WHEN** an authorized operator completes a durable privileged mutation
- **THEN** one success audit fact identifies the actor, permission, action, target, request, and committed result

#### Scenario: Audit record is persisted
- **WHEN** an audit fact has been committed
- **THEN** no application repository or API can update or delete that fact

### Requirement: Transactional Success Audit
A privileged durable mutation SHALL NOT commit successfully unless its corresponding success audit fact commits in the same PostgreSQL transaction.

#### Scenario: Audit insertion fails
- **WHEN** a privileged state mutation is valid but its success audit fact cannot be inserted
- **THEN** the entire transaction rolls back and the API does not report success

#### Scenario: Notification fails after audited mutation
- **WHEN** the durable mutation and audit fact commit but a later notification delivery fails
- **THEN** the committed action and audit fact remain valid and the notification follows its own retry path

### Requirement: Privacy-Bounded Audit Detail
Audit detail SHALL be size-bounded and action-specific and MUST NOT persist credentials, access tokens, passwords, raw media, or unbounded request and response bodies.

#### Scenario: Audit detail contains a forbidden value
- **WHEN** a privileged use case attempts to record a token or unsupported detail key
- **THEN** audit validation rejects the fact and the protected mutation does not commit

#### Scenario: Audit detail exceeds its limit
- **WHEN** serialized audit detail exceeds the configured maximum
- **THEN** the fact is rejected rather than truncated into misleading evidence

### Requirement: Stable Audit Query
Authorized audit readers SHALL be able to query a bounded time range with optional actor, action, target type, and outcome filters using stable `(created_at, id)` cursor pagination.

#### Scenario: Audit reader requests a next page
- **WHEN** an authorized caller supplies a valid cursor bound to the same filters
- **THEN** Frux returns the next records in `created_at DESC, id DESC` order without duplicates

#### Scenario: Caller lacks audit permission
- **WHEN** an authenticated principal without `audit.read` queries audit records
- **THEN** Frux returns HTTP 403 and no audit metadata

### Requirement: Audited Ordinary Account Enforcement
Frux SHALL register immutable `account.freeze`, `account.unfreeze`, and `account.sessions_revoke` audit actions using `account.manage` and the `user_account` target type. Each successful mutation SHALL commit exactly one corresponding success fact in the same PostgreSQL transaction as the account, refresh-session, and idempotency changes.

#### Scenario: Account freeze commits
- **WHEN** an authorized freeze operation commits
- **THEN** its audit fact records the actor, target user ID, action, registered reason, previous and new status, previous and new version, route, method, request ID, and idempotency-key hash

#### Scenario: Force sign-out commits
- **WHEN** an authorized force-sign-out operation commits
- **THEN** its audit fact records the unchanged account status, version transition, registered reason, and bounded revoked-session count

#### Scenario: Audit insertion fails
- **WHEN** account and session changes are valid but the success audit fact cannot be inserted
- **THEN** the account status, authentication version, refresh sessions, and idempotency result all roll back and the API does not report success

#### Scenario: Audit detail is inspected
- **WHEN** an authorized audit reader views an ordinary-account enforcement fact
- **THEN** the detail contains no login account identifier, nickname, profile text, password, token, cookie, refresh-session identifier, or raw idempotency key

#### Scenario: Idempotent replay returns
- **WHEN** a previously committed account mutation is replayed with the same idempotency key and fingerprint
- **THEN** Frux returns the stored result without appending a second success audit fact
