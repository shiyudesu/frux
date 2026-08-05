## ADDED Requirements

### Requirement: Stable Human Review Queue
Authorized reviewers SHALL be able to list pending human-review cases ordered by `priority DESC, created_at ASC, id ASC` using a cursor bound to the active filters.

#### Scenario: Reviewer requests the next queue page
- **WHEN** a reviewer supplies a valid cursor with unchanged filters
- **THEN** Frux returns the next stable queue slice without duplicates

#### Scenario: Caller lacks review-read permission
- **WHEN** an authenticated principal without `review.read` requests the queue
- **THEN** Frux returns HTTP 403 and no case evidence

### Requirement: Expiring Review Lease
A human-review case SHALL be decided only by its current authorized lease holder using an unexpired opaque lease token and expected case version.

#### Scenario: Reviewer claims an available case
- **WHEN** an authorized reviewer claims an unleased pending case
- **THEN** Frux records the reviewer, lease expiry, token hash, and incremented case version

#### Scenario: Another reviewer decides a leased case
- **WHEN** a different reviewer submits a decision for an actively leased case
- **THEN** Frux returns a stable conflict and leaves the case unchanged

#### Scenario: Lease expires
- **WHEN** server time passes the lease expiry without renewal or decision
- **THEN** the case becomes claimable again without deleting its prior assignment history

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
