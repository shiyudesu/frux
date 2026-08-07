## MODIFIED Requirements

### Requirement: Provenanced Machine Evidence
Machine-review ingestion SHALL accept only authenticated internal requests and SHALL persist bounded normalized labels, confidence values, evidence references, provider, model version, policy version, result identity, generated time, and a registered source classification of `production_provider`, `test_seed`, `recovery`, or `legacy_unknown`.

#### Scenario: Valid production machine result arrives
- **WHEN** the production moderation Worker submits a valid result for the current open case
- **THEN** immutable signal records preserve its normalized evidence, production source, generation time, provider, model, and policy provenance

#### Scenario: Manual test result arrives
- **WHEN** an authorized internal test path submits seeded evidence
- **THEN** the result is classified as `test_seed` and cannot be presented as production-model evidence

#### Scenario: Recovery result arrives
- **WHEN** provider processing is disabled or terminally unavailable
- **THEN** the result is classified as `recovery`, preserves a bounded failure reference, and cannot be presented as a model judgment

#### Scenario: Same provider result is retried
- **WHEN** the same normalized result identity is submitted again
- **THEN** Frux returns the original outcome without duplicating signals or decisions

#### Scenario: Result contains invalid bounds
- **WHEN** a result contains an unknown required field, invalid source classification, invalid confidence, excessive labels, or oversized evidence
- **THEN** Frux rejects it without changing the case or video

### Requirement: Versioned Automated Review Policy
Automated routing SHALL use a validated active policy version and a persisted rollout mode that together map registered labels and confidence thresholds to an allowed approve, reject, or human-review outcome. The rollout mode SHALL be able to restrict but not broaden the active policy outcome.

#### Scenario: High reject threshold is met in enforce mode
- **WHEN** normalized evidence meets an active reject rule and the persisted rollout mode is `enforce`
- **THEN** the case receives a policy-versioned automated reject decision

#### Scenario: Reject threshold is met in approve-only mode
- **WHEN** normalized evidence meets an active reject rule and the persisted rollout mode is `approve_only`
- **THEN** the case moves to pending human review rather than automated rejection

#### Scenario: Evidence arrives in observe mode
- **WHEN** valid production evidence is submitted under `observe`
- **THEN** the evidence is persisted and the case moves to pending human review regardless of the policy result

#### Scenario: Evidence falls into a human-review band
- **WHEN** evidence meets a human-review threshold without a higher-precedence allowed outcome
- **THEN** the case moves to pending human review and the video remains pending review

#### Scenario: Policy is invalid
- **WHEN** a policy contains unknown actions, invalid thresholds, or unsupported label rules
- **THEN** it cannot become active

### Requirement: Safe Moderation Failure
Unavailable, disabled, or malformed moderation processing SHALL leave the video review-gated, SHALL use durable bounded retry outside the request path, and SHALL route terminal current work to human review with explicit recovery provenance.

#### Scenario: Moderation provider is temporarily unavailable
- **WHEN** the production job encounters a retryable provider failure
- **THEN** the current case and video remain non-public while durable work retries within hard bounds

#### Scenario: Moderation provider remains unavailable
- **WHEN** bounded attempts are exhausted for the current case
- **THEN** one recovery result routes the case to pending human review without claiming that a model approved or rejected the content

#### Scenario: Moderation integration is disabled
- **WHEN** a reviewable case is processed with rollout mode `disabled`
- **THEN** Frux does not call an external provider and safely routes the case to human review with disabled-mode recovery provenance
