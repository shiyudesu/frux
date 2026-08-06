# automated-content-review Specification

## Purpose

Define reliable, provenanced, policy-driven automated content review while keeping moderation failures safe.

## Requirements

### Requirement: Idempotent Review Case Intake
Frux SHALL create at most one active review case for each `(video_id, review_version)` after the video has reviewable media.

#### Scenario: Media-ready event is delivered twice
- **WHEN** duplicate events request review intake for the same video review version
- **THEN** both deliveries resolve to the same review case without duplicate work

#### Scenario: Pending video has no case
- **WHEN** reconciliation finds a pending-review video with ready media and no case
- **THEN** it creates the missing case and records the recovery outcome

### Requirement: Provenanced Machine Evidence
Machine-review ingestion SHALL accept only authenticated internal requests and SHALL persist bounded normalized labels, confidence values, evidence references, provider, model version, policy version, and result identity.

#### Scenario: Valid machine result arrives
- **WHEN** an internal caller submits a valid result for the current open case
- **THEN** immutable signal records preserve its normalized evidence and provenance

#### Scenario: Same provider result is retried
- **WHEN** the same normalized result identity is submitted again
- **THEN** Frux returns the original outcome without duplicating signals or decisions

#### Scenario: Result contains invalid bounds
- **WHEN** a result contains an unknown required field, invalid confidence, excessive labels, or oversized evidence
- **THEN** Frux rejects it without changing the case or video

### Requirement: Versioned Automated Review Policy
Automated routing SHALL use a validated active policy version that maps registered labels and confidence thresholds to approve, reject, or human-review outcomes.

#### Scenario: High reject threshold is met
- **WHEN** normalized evidence meets an active reject rule
- **THEN** the case receives a policy-versioned automated reject decision

#### Scenario: Evidence falls into a human-review band
- **WHEN** evidence meets a human-review threshold without a higher-precedence reject rule
- **THEN** the case moves to pending human review and the video remains pending review

#### Scenario: Policy is invalid
- **WHEN** a policy contains unknown actions, invalid thresholds, or unsupported label rules
- **THEN** it cannot become active

### Requirement: Atomic Automated Outcome
An automated approve or reject outcome SHALL persist the decision, update the case, and apply the matching video lifecycle transition in one transaction.

#### Scenario: Video state conflicts with result
- **WHEN** a machine result targets a stale review version or terminal video state
- **THEN** the transaction does not apply the automated outcome

#### Scenario: Automated approval commits
- **WHEN** a current case satisfies an approval rule and all writes succeed
- **THEN** the decision, closed case, and published video commit together

### Requirement: Safe Moderation Failure
Unavailable or malformed moderation processing SHALL leave the video review-gated and SHALL produce bounded retry and failure observability.

#### Scenario: Moderation provider is unavailable
- **WHEN** the external producer cannot generate a result
- **THEN** the case remains open, the video remains non-public, and retry is bounded outside the request path
