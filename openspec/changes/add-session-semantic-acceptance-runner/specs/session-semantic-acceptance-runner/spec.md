## ADDED Requirements

### Requirement: Validation is non-mutating by default
The runner SHALL default to validation-only mode and SHALL require both an explicit execution option
and an independent mutation acknowledgement before login, policy creation, behavior submission, Feed
requests, or cleanup mutation.

#### Scenario: Execution confirmation is incomplete
- **WHEN** either mutation gate is absent
- **THEN** the runner validates bounded configuration and existing fixture prerequisites, reports planned stages, and performs no state-changing request

### Requirement: Existing active-contract fixtures are required
The runner SHALL require explicit positive seed, negative seed, and expected target videos that are
currently readable and have matching active-contract Fact and Projection evidence. It SHALL NOT
upload media, enqueue embeddings, or call an external model.

#### Scenario: Fixtures are compatible
- **WHEN** all configured videos are readable under the selected contract and the expected target is a positive Exact neighbor of the positive seed
- **THEN** the runner may enter the confirmed mutation workflow

#### Scenario: Fixture evidence is incomplete
- **WHEN** a video is unreadable, missing a current Fact/Projection, contract-mismatched, or the target is not a positive Exact result
- **THEN** the runner stops before policy or behavior mutation with a closed prerequisite result

### Requirement: Temporary policy lifecycle is scoped and reversible
The runner SHALL create one unique Domain-valid one-percent Recommendation policy, derive a request ID
that selects its cohort, report its identity, and disable exactly that runner-owned policy on every
post-creation exit path. Existing policies MUST NOT be rewritten or disabled.

#### Scenario: Acceptance policy is installed
- **WHEN** confirmed execution begins after preflight
- **THEN** the policy selects `semantic_session`, positive `semantic_similarity`, complete quota fields, full sampling, and the exact active contract

#### Scenario: Run succeeds or fails after policy creation
- **WHEN** execution leaves any later stage
- **THEN** the exact created policy ID is disabled while pre-existing policy states remain unchanged

#### Scenario: Optional cleanup is requested
- **WHEN** evidence collection is complete and cleanup is enabled
- **THEN** the runner may delete only its disabled policy row and revert only its run-created favorite state

### Requirement: Acceptance uses authenticated behavior and Feed workflows
The runner SHALL use normal consumer login, identity, view-event, favorite, unfavorite, and Feed query
APIs with run-scoped event, playback, request, session, and idempotency identities. It SHALL use
PostgreSQL only for policy lifecycle and bounded evidence reads.

#### Scenario: Trusted session facts are created
- **WHEN** the runner submits positive completion/favorite and negative early-skip facts
- **THEN** the API persists them through existing validation and the first Feed request uses the configured positive current and negative recent context

#### Scenario: Same run request is retried
- **WHEN** a network retry repeats a run-scoped event or favorite mutation
- **THEN** existing idempotency returns stable results without duplicate semantic weight

### Requirement: Real semantic evidence is verified
The runner SHALL verify the temporary policy version, builder/contract identity, finite positive
Confidence, positive/negative/compatible counts, expected target `semantic_session` reason, positive
`semantic_similarity`, bounded quota diagnostics, and absence of raw vector/event fields in sampled
request evidence.

#### Scenario: First recommendation page completes
- **WHEN** Session Semantic executes successfully for the acceptance request
- **THEN** the sampled request log contains the expected bounded semantic evidence and target participation without exposing vector components

### Requirement: Snapshot continuation does not recompute semantics
The runner SHALL require a first response with a signed continuation cursor, request the next page
under the same request/session identity, and verify API metric deltas show no additional Session
Semantic builder or provider execution on that page.

#### Scenario: Snapshot page is requested
- **WHEN** the first page created a valid Snapshot and returned `next_cursor`
- **THEN** the next page succeeds from the stored ordering while builder/provider operation counters remain unchanged

#### Scenario: Fixture pool cannot produce a cursor
- **WHEN** the first response has no continuation cursor
- **THEN** the runner reports a closed Snapshot prerequisite failure rather than weakening the acceptance assertion

### Requirement: Report and model-call evidence are safe
The runner SHALL emit a versioned `technical_acceptance` JSON report with closed stage results,
bounded identities/counts/deltas, cleanup status, and `external_model_calls: 0`. Credentials, tokens,
DSNs, passwords, cursors, raw bodies, raw events, and raw vectors MUST NOT appear in stdout, stderr,
report files, or normal errors.

#### Scenario: Adapter metrics are configured
- **WHEN** the runner can scrape bounded adapter operation counters before and after execution
- **THEN** video/query/startup deltas remain zero or the run fails model-call verification

#### Scenario: Report file is written
- **WHEN** the operator requests a report path
- **THEN** the complete redacted report is atomically written with mode `0600`
