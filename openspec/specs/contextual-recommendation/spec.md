# contextual-recommendation Specification

## Purpose
Define bounded, resilient, explainable contextual recommendations with stable pagination, feedback, evaluation, and operational fallbacks.

## Requirements

### Requirement: Bounded Recommendation Context
Recommendation Feed queries SHALL accept a typed bounded context while deriving trusted identity, relationship, interaction, and exposure facts on the server.

#### Scenario: Client submits valid session context
- **WHEN** a recommendation query includes bounded request/session identifiers, refresh index, recent video IDs, and normalized playback capabilities
- **THEN** the service uses the supported fields for recall, ranking, pagination, and logging

#### Scenario: Client submits excessive context
- **WHEN** a context string, list, number, or unknown field exceeds the documented contract
- **THEN** the request is rejected or normalized consistently without unbounded storage or computation

### Requirement: Multi-Source Candidate Recall
The recommendation service SHALL merge bounded candidates from configurable fresh, hot, content-similar, followed-author, and session-continuation recall providers.

#### Scenario: One recall provider fails
- **WHEN** a provider exceeds its deadline or returns an infrastructure error
- **THEN** the service continues with healthy providers and marks the request degraded

#### Scenario: Providers return the same video
- **WHEN** multiple providers recall one video
- **THEN** the pool contains one candidate retaining its applicable recall reasons and features

#### Scenario: Candidate is no longer public
- **WHEN** a recalled video becomes private, deleted, or non-published before response assembly
- **THEN** it is removed from the response

### Requirement: Time-Decayed User Interest Profile
Frux SHALL maintain a versioned recommendation profile from idempotent positive, negative, relational, and session behavior signals with time decay.

#### Scenario: User completes related videos
- **WHEN** reliable completion and sustained-progress events are consumed
- **THEN** the corresponding content and author affinities increase according to the active profile policy

#### Scenario: User skips early
- **WHEN** a reliable early-skip or explicit negative event is consumed
- **THEN** the related candidate features receive a bounded negative contribution

#### Scenario: Profile event is delivered twice
- **WHEN** the same source event is consumed more than once
- **THEN** the profile applies it at most once

### Requirement: Explicit Recommendation Feedback
Authenticated users SHALL be able to submit idempotent bounded recommendation feedback for a video and recommendation request.

#### Scenario: User selects not interested
- **WHEN** the user submits `not_interested` for a recommended video
- **THEN** the feedback is stored and the video or related preference is suppressed according to policy

#### Scenario: Same feedback is retried
- **WHEN** the user repeats the same normalized feedback with the same idempotency key
- **THEN** the API returns the original result without duplicating preference changes

### Requirement: Versioned Ranking Policy
Recommendation ranking SHALL use a validated versioned policy defining recall budgets, feature weights, decay, exposure windows, diversity, and rollout.

#### Scenario: Invalid policy is submitted
- **WHEN** a policy contains unknown features, invalid bounds, or unsupported values
- **THEN** it cannot become active

#### Scenario: Policy rollback occurs
- **WHEN** operators select a previous valid policy version
- **THEN** new recommendation requests use that version without redeploying the API

### Requirement: Stable Recommendation Pagination
The normal recommendation path SHALL preserve one ordered candidate snapshot for the lifetime of a bounded request session and SHALL use a signed snapshot cursor.

#### Scenario: User requests a second page
- **WHEN** the snapshot remains available and the cursor is valid
- **THEN** the service returns the next ordered slice without recomputing earlier ranks

#### Scenario: Snapshot candidate changes visibility
- **WHEN** a candidate becomes unreadable after snapshot creation
- **THEN** response assembly omits it without exposing private metadata

#### Scenario: Snapshot storage is unavailable
- **WHEN** Redis cannot provide recommendation snapshots
- **THEN** the service uses the documented deterministic degraded cursor path and reports degraded operation

### Requirement: Recommendation Evaluation Records
Frux SHALL record sampled privacy-bounded request, policy, candidate-reason, and outcome linkage data using recommendation request IDs.

#### Scenario: Sampled request is served
- **WHEN** a request is selected by the configured evaluation sampling policy
- **THEN** its policy version, bounded context, ordered candidates, reasons, and degraded flags are retained for the configured period

#### Scenario: Outcome event arrives
- **WHEN** an exposure, progress, completion, skip, or interaction carries a recorded recommendation request ID
- **THEN** offline evaluation can link the outcome without storing client tokens or signed media URLs

### Requirement: Operational Model Fallback
Recommendation SHALL preserve a local content-vector fallback behind a versioned model interface.

#### Scenario: Preferred embedding is unavailable
- **WHEN** the preferred semantic model or vectors are unavailable
- **THEN** ranking can use the supported local hash-vector or non-vector fallback without failing the Feed

### Requirement: Recommendation Scene Context Isolation
The Web client SHALL keep Recommendation request identity, session identity, signed pagination state, recent-video context, and accepted-feedback suppression scoped to the Recommendation scene. Temporarily activating another Feed scene MUST NOT reset or contaminate a valid retained Recommendation session.

#### Scenario: User leaves and returns to Recommendation
- **WHEN** the user leaves a committed Recommendation scene for another Feed route and returns while its snapshot remains valid
- **THEN** the client restores the same request, session, cursor, active video, and suppression state without starting another recommendation query

#### Scenario: Recommendation is intentionally refreshed
- **WHEN** the client creates a new Recommendation request after an explicit refresh
- **THEN** `recent_video_ids` and `current_video_id` are derived only from retained Recommendation cards and the Recommendation refresh index advances within its logical session

#### Scenario: Another Feed scene was active
- **WHEN** Timeline, Following, or Hot is the scene being left before Recommendation loads or refreshes
- **THEN** cards and indices from that scene do not appear in Recommendation recent-video or current-video context

#### Scenario: Recommendation feedback was accepted before leaving
- **WHEN** a video or author was suppressed by accepted recommendation feedback and the user temporarily visits another Feed scene
- **THEN** returning to Recommendation keeps that suppression for the retained recommendation session
