# human-review-workflow Specification

## Purpose

Define stable human-review work selection, expiring reviewer leases, atomic decisions, and immutable review history.

## Requirements

### Requirement: Stable Human Review Queue
Authorized reviewers SHALL be able to list pending human-review cases ordered by `priority DESC, created_at ASC, id ASC` using a cursor bound to the active filters.

#### Scenario: Reviewer requests the next queue page
- **WHEN** a reviewer supplies a valid cursor with unchanged filters
- **THEN** Frux returns the next stable queue slice without duplicates

#### Scenario: Policy routes cases to human review
- **WHEN** machine evidence produces a human outcome
- **THEN** Frux atomically persists a deterministic priority from `1` through `100` and higher-confidence human signals sort first

#### Scenario: More than one recovery batch of leases has expired
- **WHEN** a reviewer requests the queue
- **THEN** all expired leases are considered available by database time without depending on a fixed-size recovery pass

#### Scenario: Pending case subject is no longer reviewable
- **WHEN** a video's lifecycle is terminal or its review version no longer matches a pending-human case
- **THEN** Frux excludes that case from queue pages and queue metrics

#### Scenario: Caller lacks review-read permission
- **WHEN** an authenticated principal without `review.read` requests the queue
- **THEN** Frux returns HTTP 403 and no case evidence

### Requirement: Expiring Review Lease
A human-review case SHALL be claimed, renewed, released, or decided only with the expected case version, and SHALL be decided only by its current authorized lease holder using an unexpired opaque lease token. Lease validity SHALL be evaluated using database/server time rather than reviewer-supplied time.

#### Scenario: Reviewer claims an available case
- **WHEN** an authorized reviewer claims an unleased pending case
- **THEN** Frux records the reviewer, lease expiry, token hash, and incremented case version

#### Scenario: Current holder renews a lease
- **WHEN** the current reviewer supplies the unexpired lease token and expected case version
- **THEN** Frux extends the lease by the bounded duration, increments the case version, and records immutable renewal history

#### Scenario: Current holder releases a lease
- **WHEN** the current reviewer supplies the unexpired lease token and expected case version to release the case
- **THEN** Frux clears the lease, increments the case version, records immutable release history, and makes the case available

#### Scenario: Lease mutation uses a stale case version
- **WHEN** a reviewer claims, renews, releases, or decides with a case version that is no longer current
- **THEN** Frux returns a stable version conflict and leaves the case unchanged

#### Scenario: Another reviewer decides a leased case
- **WHEN** a different reviewer submits a decision for an actively leased case
- **THEN** Frux returns a stable conflict and leaves the case unchanged

#### Scenario: Lease expires
- **WHEN** server time passes the lease expiry without renewal or decision
- **THEN** the case becomes claimable again without deleting its prior assignment history

#### Scenario: Reviewer clocks differ
- **WHEN** a reviewer clock disagrees with database/server time
- **THEN** Frux evaluates claim, renewal, release, and decision lease validity only from database/server time

#### Scenario: Reviewer claims a stale subject
- **WHEN** a claim locks a pending-human case whose video is terminal or has a newer review version
- **THEN** Frux atomically retires the case as cancelled or superseded with immutable history and returns a conflict without creating a lease

### Requirement: Idempotent Human Decision
Human decisions SHALL be limited to approve or reject, require a registered reason code, accept only a bounded optional note, and support payload-bound idempotency.

#### Scenario: Reviewer repeats the same decision
- **WHEN** the same reviewer repeats a normalized decision with the same idempotency key
- **THEN** Frux returns the original result without adding another decision

#### Scenario: Idempotency key is reused with another outcome
- **WHEN** the reviewer reuses a decision idempotency key for a different reason, note, or outcome
- **THEN** Frux returns HTTP 409 and preserves the original decision

### Requirement: Atomic Human Enforcement and Audit
A valid human decision SHALL commit the immutable decision, final case state, matching video transition, success audit fact, and notification outbox fact in one transaction.

#### Scenario: Audit insertion fails
- **WHEN** the review and video writes are valid but the audit fact cannot be inserted
- **THEN** none of the decision, case, video, or notification writes commit

#### Scenario: Approved subject version is stale
- **WHEN** the video's current review version differs from the leased case
- **THEN** the decision is rejected as a conflict and the newer subject is not published

### Requirement: Immutable Review History
Authorized review readers SHALL receive machine evidence, automated decisions, assignments, lease events, and human decisions as ordered immutable history.

#### Scenario: Reviewer opens case detail
- **WHEN** an authorized reviewer requests a case
- **THEN** Frux returns the bounded current subject plus prior evidence and decisions without exposing credentials or mutable raw provider payloads
