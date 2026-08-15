## ADDED Requirements

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

