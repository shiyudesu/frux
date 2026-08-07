## ADDED Requirements

### Requirement: Durable Moderation Work
Frux SHALL create at most one durable moderation job for each current review case, review version, and provider-configuration version, and SHALL process jobs with database-time leases, bounded attempts, stable result identity, and reconciliation.

#### Scenario: Reviewable case is created
- **WHEN** a current video version becomes reviewable under a rollout mode that calls the provider
- **THEN** Frux commits one pending moderation job associated with that case and version

#### Scenario: Intake or event is delivered twice
- **WHEN** duplicate work requests target the same case, review version, and provider configuration
- **THEN** Frux resolves them to the same moderation job

#### Scenario: Worker lease expires
- **WHEN** a Worker does not finish before the database-time job lease expires
- **THEN** reconciliation makes the job retryable without changing its deterministic result identity

#### Scenario: Subject becomes stale
- **WHEN** the video's current review version or lifecycle no longer matches a pending moderation job
- **THEN** Frux cancels the job and does not submit its result

### Requirement: Bounded Protected Moderation Inputs
Frux SHALL derive deterministic bounded moderation inputs from protected reviewable media and SHALL NOT disclose the original media as a reusable public resource.

#### Scenario: Worker prepares visual input
- **WHEN** a valid current job has reviewable media
- **THEN** Frux produces at most 12 deterministically distributed JPEG frames, each no larger than 512 pixels on its longest edge, within an 8 MiB total budget

#### Scenario: Gateway reads a sample
- **WHEN** the configured gateway processes a claimed job
- **THEN** it receives only short-lived protected sample access plus bounded title/description metadata

#### Scenario: Sample retention expires
- **WHEN** a moderation sample has been accepted or exceeds its configured retention
- **THEN** Frux durably schedules or completes deletion without deleting the source media

#### Scenario: Input extraction fails
- **WHEN** probing or frame generation fails
- **THEN** the job records a bounded retryable or terminal error and no incomplete input manifest is submitted

### Requirement: Authenticated Provider Exchange
Frux SHALL call the configured production inference gateway with a signed, time-bounded, idempotent request and SHALL strictly validate the bounded canonical response before accepting machine evidence.

#### Scenario: Gateway returns a valid result
- **WHEN** the response signature/transport, provider, model, generation time, labels, confidence values, and frame timestamps satisfy the contract
- **THEN** Frux submits a deterministic production-provider machine result through the existing review application boundary

#### Scenario: Gateway request is retried
- **WHEN** a timeout leaves the response outcome uncertain
- **THEN** Frux retries with the same stable request/result identity and cannot create duplicate signals or decisions

#### Scenario: Gateway response is malformed
- **WHEN** the response has unknown required fields, invalid bounds, an impossible timestamp, or excessive signals
- **THEN** Frux rejects the response, records a bounded failure, and leaves the video review-gated

#### Scenario: Gateway credentials are unavailable
- **WHEN** a provider-enabled mode starts without the required endpoint or secret
- **THEN** configuration validation fails closed and Frux does not send unsigned moderation traffic

### Requirement: Conservative Moderation Rollout
Frux SHALL apply a persisted rollout mode of `disabled`, `observe`, `approve_only`, or `enforce` to each machine result, and SHALL never make a more permissive automated decision than the selected mode allows.

#### Scenario: Mode is disabled
- **WHEN** a new reviewable case is processed while moderation mode is disabled
- **THEN** Frux skips the external call and deterministically routes the case to human review with recovery provenance

#### Scenario: Mode is observe
- **WHEN** valid production evidence arrives in observe mode
- **THEN** Frux stores the evidence and routes the case to human review regardless of the active policy's automated outcome

#### Scenario: Mode is approve only
- **WHEN** valid evidence satisfies an automated approval rule in approve-only mode
- **THEN** Frux may approve, while policy reject or human outcomes remain pending human review

#### Scenario: Mode is enforce
- **WHEN** valid evidence is evaluated in enforce mode
- **THEN** Frux applies the complete active approve/reject/human policy

#### Scenario: Rollout mode changes
- **WHEN** an operator changes the configured mode after a result was routed
- **THEN** the existing immutable result and decision retain their original mode and are not retroactively reclassified

### Requirement: Human Fallback on Terminal Failure
Frux SHALL keep provider failures non-public, retry transient failures within configured hard bounds, and route exhausted or disabled work to human review using explicit recovery provenance rather than fabricated model evidence.

#### Scenario: Provider is temporarily unavailable
- **WHEN** the gateway times out, rate-limits, or returns a retryable service error with attempts remaining
- **THEN** the job enters bounded retry wait and the video remains pending and non-public

#### Scenario: Provider attempts are exhausted
- **WHEN** a current job reaches its terminal attempt or a registered non-retryable failure
- **THEN** Frux submits one deterministic recovery result that routes the case to human review and identifies that no production judgment was obtained

#### Scenario: Terminal fallback is retried
- **WHEN** recovery result delivery is repeated after an uncertain response
- **THEN** the existing recovery result and human-routing outcome are returned without duplicates
