# user-account-management-console Specification

## Purpose

Defines privacy-bounded discovery and audited enforcement workflows for privileged management of ordinary consumer accounts.

## Requirements

### Requirement: Ordinary Consumer Account Boundary
Frux SHALL expose the account-management console and APIs only for accounts whose current persisted role is exactly `user`. Reviewer, operator, admin, and unknown-role accounts MUST NOT appear in results or accept account-management mutations.

#### Scenario: Administrator lists ordinary users
- **WHEN** an authorized administrator requests the account list
- **THEN** Frux returns matching accounts whose current role is `user` and excludes every privileged or unknown role

#### Scenario: Privileged account is requested directly
- **WHEN** an authorized administrator requests an account-management detail or mutation for a reviewer, operator, admin, or unknown-role account
- **THEN** Frux returns the stable ordinary-user-not-found result and performs no mutation

#### Scenario: User is promoted concurrently
- **WHEN** an account was listed as `user` but its role changes before an account-management mutation commits
- **THEN** the mutation fails without changing its status, authentication version, sessions, or audit history

### Requirement: Stable Ordinary User Discovery
Authorized administrators SHALL be able to list ordinary accounts by user identifier, canonical account identifier, nickname query, and account status using a cursor bound to the complete filter set. Results SHALL use stable `created_at DESC, id DESC` ordering, a default limit of 20, and a maximum limit of 100.

#### Scenario: Administrator searches by private account identifier
- **WHEN** an authorized administrator submits a canonical account query
- **THEN** Frux may match the private login identifier inside the privileged boundary and returns no password or credential material

#### Scenario: Administrator requests a next page
- **WHEN** a valid cursor is supplied with the same filters
- **THEN** Frux returns the next ordinary accounts without duplicates in `created_at DESC, id DESC` order

#### Scenario: Cursor filters change
- **WHEN** a cursor created for one account, nickname, identifier, or status filter is submitted with different filters
- **THEN** Frux rejects the cursor instead of mixing result sets

### Requirement: Privacy-Bounded Account Detail
The privileged account detail SHALL return the ordinary user's canonical account identifier, public profile fields, status, creation and update times, aggregate relationship and content statistics, a concurrency version, and an active durable-session count. It MUST NOT return password hashes, refresh secrets, refresh-session identifiers, access tokens, cookies, or protected-media credentials.

#### Scenario: Administrator opens ordinary account detail
- **WHEN** an authorized administrator opens a matching ordinary account
- **THEN** Frux returns the bounded account and aggregate view required for operational decisions

#### Scenario: Account has multiple devices
- **WHEN** an ordinary account has multiple active refresh sessions
- **THEN** the response may return only their aggregate count and does not expose individual session identifiers or secrets

### Requirement: Reasoned Account Freeze and Unfreeze
An authorized administrator SHALL be able to freeze a normal ordinary account or unfreeze a frozen ordinary account only with a registered reason code, the current expected version, and an `Idempotency-Key` no longer than 128 characters. A committed mutation SHALL increment the account authentication version, append a durable lifecycle-notification outbox fact, and return the resulting status, version, revoked-session count, and replay state.

#### Scenario: Normal account is frozen
- **WHEN** an authorized administrator submits a valid freeze command against the current version
- **THEN** the account becomes frozen, its authentication version increments, its active refresh sessions are revoked, and the committed result, audit fact, and freeze-notification outbox fact are stored atomically

#### Scenario: Frozen account is restored
- **WHEN** an authorized administrator submits a valid unfreeze command against the current version
- **THEN** the account becomes normal, its authentication version increments, previously revoked sessions remain revoked, and the committed result, audit fact, and unfreeze-notification outbox fact are stored atomically

#### Scenario: Cancelled account is mutated
- **WHEN** an administrator attempts to freeze or unfreeze an account in the cancelled state
- **THEN** Frux returns a stable state conflict and leaves the account unchanged

#### Scenario: Expected version is stale
- **WHEN** another account or credential mutation has changed the version before the command commits
- **THEN** Frux returns a stable version conflict and writes no account, session, idempotency, success-audit, or notification-outbox change

#### Scenario: Idempotent command is repeated
- **WHEN** the same actor repeats the same command with the same idempotency key and request fingerprint
- **THEN** Frux returns the original committed result with `replayed=true` without applying the mutation, audit fact, outbox fact, or user message again

#### Scenario: Idempotency key is reused differently
- **WHEN** the same actor reuses an idempotency key for a different account, action, version, or reason
- **THEN** Frux returns a stable idempotency conflict and performs no mutation

### Requirement: Administrative Force Sign-Out
An authorized administrator SHALL be able to revoke all active durable consumer sessions for a normal or frozen ordinary account with a registered reason, current expected version, and bounded idempotency key. The operation SHALL preserve account status, increment authentication version, and revoke active refresh sessions transactionally.

#### Scenario: Active account is forced to sign out
- **WHEN** an authorized administrator commits a force-sign-out command
- **THEN** all active refresh sessions are revoked, the account status is unchanged, the authentication version increments, and the operation is audited

#### Scenario: Account has no active sessions
- **WHEN** a valid force-sign-out command targets an ordinary account with no active refresh sessions
- **THEN** Frux still commits the new authentication version and auditable operation with a revoked-session count of zero

#### Scenario: Force sign-out commits
- **WHEN** an administrator completes a force-sign-out operation without changing frozen status
- **THEN** Frux does not create a freeze or unfreeze lifecycle notification

### Requirement: Durable Account Lifecycle Messages
Frux SHALL create one idempotent `SYSTEM` message for every committed ordinary-account freeze and unfreeze transition through an account-owned transactional outbox and a supervised Worker. The message title and content SHALL be derived only from the registered operation and safe reason-code mapping.

#### Scenario: Freeze message is delivered before old access expires
- **WHEN** a freeze outbox fact is delivered while a previously issued short access token remains valid
- **THEN** the user can read an unread “账号已被冻结” message containing the registered safe reason through the existing message center

#### Scenario: Freeze message is not read during residual access
- **WHEN** the prior access token expires before the freeze message is delivered or read
- **THEN** the message remains durable and becomes visible after the account is unfrozen and the user completes a new login

#### Scenario: Unfreeze message is delivered
- **WHEN** an unfreeze outbox fact is delivered
- **THEN** the user receives one unread “账号已解冻” message containing the registered safe reason

#### Scenario: Worker retries delivery
- **WHEN** message creation fails transiently
- **THEN** the Worker retains the outbox fact and retries with a bounded lease and exponential backoff

#### Scenario: Lifecycle event is redelivered
- **WHEN** an outbox claim is repeated after Worker restart or an uncertain response
- **THEN** the stable event and idempotency identity produce at most one `user_message`

#### Scenario: Lifecycle payload is invalid
- **WHEN** an outbox fact contains an unsupported operation, reason, recipient, or event identity
- **THEN** the Worker classifies it as terminal instead of retrying indefinitely or creating free-form content

### Requirement: Account and Content Enforcement Separation
Account freeze, unfreeze, and force-sign-out operations SHALL NOT change video lifecycle, visibility, media delivery, comments, relationships, existing messages, profile fields, or aggregate content counts. Freeze and unfreeze MAY append only their defined account lifecycle message.

#### Scenario: Published creator account is frozen
- **WHEN** an administrator freezes an ordinary account that owns published videos
- **THEN** the account can no longer establish or refresh login sessions, while the videos retain their existing lifecycle and visibility until a separate content-enforcement action changes them

### Requirement: Account Management Web Workflow
The Admin Shell SHALL provide a permission-filtered `/admin/accounts` destination with searchable and filterable ordinary-account results, bounded detail, explicit freeze, unfreeze, and force-sign-out confirmations, and truthful loading, empty, validation, conflict, forbidden, unavailable, replayed, and success states.

#### Scenario: Administrator opens account management
- **WHEN** the current server-confirmed principal has the account-management permission
- **THEN** the Admin Shell displays the account destination and loads only ordinary consumer accounts

#### Scenario: Operator lacks account-management permission
- **WHEN** a reviewer or operator navigates directly to `/admin/accounts`
- **THEN** the backend returns the authoritative forbidden result and the Web client displays no cached account data

#### Scenario: Freeze confirmation is shown
- **WHEN** an administrator initiates account freeze
- **THEN** the Web client explains that login and refresh will be blocked, existing access may remain until its short expiration, and existing content will not be automatically hidden
