# human-review-workflow Specification

## Purpose

Define stable human-review work selection, expiring reviewer leases, atomic decisions, and immutable review history.

## Requirements

### Requirement: Stable Human Review Queue
Authorized reviewers SHALL be able to list pending human-review cases in `available`, `mine`, and `recent` scopes using stable cursors bound to the selected scope and active filters. Available work SHALL be ordered by `priority DESC, created_at ASC, id ASC`; current-reviewer work SHALL use a deterministic lease-expiry order; recently completed work SHALL use a deterministic decision-time order.

#### Scenario: Reviewer requests the next available page
- **WHEN** a reviewer supplies a valid available-scope cursor with unchanged filters
- **THEN** Frux returns the next stable claimable queue slice without duplicates

#### Scenario: Reviewer requests current work
- **WHEN** an authorized reviewer selects the mine scope
- **THEN** Frux returns pending-human cases with an unexpired lease currently held by that reviewer and does not return another reviewer's work

#### Scenario: Reviewer requests recently completed work
- **WHEN** an authorized reviewer selects the recent scope
- **THEN** Frux returns a bounded stable page of cases decided by that reviewer in reverse decision order

#### Scenario: Cursor scope changes
- **WHEN** a cursor created for one queue scope is submitted for another scope
- **THEN** Frux rejects the cursor without mixing result sets

#### Scenario: Policy routes cases to human review
- **WHEN** machine evidence produces a human outcome
- **THEN** Frux atomically persists a deterministic priority from `1` through `100` and higher-confidence human signals sort first

#### Scenario: More than one recovery batch of leases has expired
- **WHEN** a reviewer requests the available scope
- **THEN** all expired leases are considered available by database time without depending on a fixed-size recovery pass

#### Scenario: Pending case subject is no longer reviewable
- **WHEN** a video's lifecycle is terminal or its review version no longer matches a pending-human case
- **THEN** Frux excludes that case from active queue pages and queue metrics

#### Scenario: Caller lacks review-read permission
- **WHEN** an authenticated principal without `review.read` requests any queue scope
- **THEN** Frux returns HTTP 403 and no case evidence

### Requirement: Expiring Review Lease
A human-review case SHALL be claimed, resumed, renewed, released, or decided only with the expected case version, and SHALL be decided only by its current authorized lease holder using an unexpired opaque lease token. Lease validity SHALL be evaluated using database/server time rather than reviewer-supplied time, and resume SHALL rotate rather than recover the prior plaintext token.

#### Scenario: Reviewer claims an available case
- **WHEN** an authorized reviewer claims an unleased pending case
- **THEN** Frux records the reviewer, lease expiry, token hash, and incremented case version

#### Scenario: Current holder resumes after losing the token
- **WHEN** the current reviewer supplies the expected case and review versions for an unexpired assignment they own
- **THEN** Frux invalidates the prior token, returns a new opaque token once, extends the bounded lease, increments case version, and records immutable resume history

#### Scenario: Another reviewer attempts resume
- **WHEN** a reviewer attempts to resume a case actively held by another reviewer
- **THEN** Frux returns a stable conflict and does not rotate or disclose the lease token

#### Scenario: Current holder renews a lease
- **WHEN** the current reviewer supplies the unexpired lease token and expected case version
- **THEN** Frux extends the lease by the bounded duration, increments the case version, and records immutable renewal history

#### Scenario: Current holder releases a lease
- **WHEN** the current reviewer supplies the unexpired lease token and expected case version to release the case
- **THEN** Frux clears the lease, increments the case version, records immutable release history, and makes the case available

#### Scenario: Lease mutation uses a stale case version
- **WHEN** a reviewer claims, resumes, renews, releases, or decides with a case version that is no longer current
- **THEN** Frux returns a stable version conflict and leaves the case unchanged

#### Scenario: Another reviewer decides a leased case
- **WHEN** a different reviewer submits a decision for an actively leased case
- **THEN** Frux returns a stable conflict and leaves the case unchanged

#### Scenario: Lease expires
- **WHEN** server time passes the lease expiry without renewal or decision
- **THEN** the case becomes claimable again without deleting its prior assignment history

#### Scenario: Reviewer clocks differ
- **WHEN** a reviewer clock disagrees with database/server time
- **THEN** Frux evaluates claim, resume, renewal, release, and decision lease validity only from database/server time

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
Authorized review readers SHALL receive machine evidence, automated decisions, assignments, claim/resume/renew/release events, and human decisions as ordered immutable history, with bounded provider, model, policy, label, confidence, and evidence-reference provenance suitable for truthful presentation.

#### Scenario: Reviewer opens case detail
- **WHEN** an authorized reviewer requests a case
- **THEN** Frux returns the bounded current subject plus prior evidence and decisions without exposing credentials or mutable raw provider payloads

#### Scenario: Seeded machine evidence is displayed
- **WHEN** a machine result uses the reserved manual seed provider
- **THEN** the review detail identifies it as test evidence rather than implying that a production model generated it

#### Scenario: Arbitrary evidence reference is returned
- **WHEN** a provider evidence reference is not a recognized internal review reference
- **THEN** Frux preserves it as bounded text and the Web client does not turn it into an automatically navigable external link

### Requirement: Separate Protected Cover Inspection
Authorized review readers SHALL be able to inspect the current review subject's protected cover independently from video playback using the same bounded preview authorization and expiry.

#### Scenario: Reviewer opens a subject with a cover
- **WHEN** an authorized reviewer loads the current review detail and protected cover access is available
- **THEN** the Web displays a separately labeled cover inspection surface in addition to using the cover as the video poster

#### Scenario: Cover access expires
- **WHEN** the protected review preview is refreshed before expiry
- **THEN** both video and cover inspection use the refreshed authorized URLs without changing public media state

#### Scenario: Cover is unavailable
- **WHEN** the subject has no resolvable protected cover
- **THEN** the review detail shows an explicit cover-unavailable state while preserving video evidence and review actions

#### Scenario: Review authorization becomes stale
- **WHEN** permission, case version, or subject lifecycle no longer permits preview
- **THEN** both video and cover access are removed and no stale protected cover remains rendered
