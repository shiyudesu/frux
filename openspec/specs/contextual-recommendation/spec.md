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

### Requirement: Complete bounded pre-rank candidate pool
For a normal policy-driven recommendation request, Frux SHALL pass every unique candidate returned
within the selected Provider budgets through visibility revalidation and unified feature scoring
before any global pool reduction. The pre-rank pool SHALL be bounded by the validated sum of
selected Provider budgets and an absolute maximum of 500 candidates, and MUST NOT be globally
ordered or truncated by `published_at`, video ID, Provider completion order, or response page size
before ranking.

#### Scenario: Small response page selects multiple Providers
- **WHEN** a 10-card request selects five Providers with budgets of 100 each and they return 500 distinct readable candidates
- **THEN** all 500 candidates are eligible for unified feature scoring before final ranking and page slicing

#### Scenario: Older candidate has stronger ranking features
- **WHEN** an older candidate is within its Provider budget and would outrank newer candidates after policy feature scoring
- **THEN** recency-only pre-rank processing cannot discard it before its features are evaluated

#### Scenario: Providers return the same video
- **WHEN** multiple Providers return one video within their budgets
- **THEN** the pre-rank pool contains one candidate retaining every valid Provider reason and source score, and the duplicate does not consume another global slot

#### Scenario: Selected budgets exceed the absolute pool bound
- **WHEN** a policy without an accepted quota-merge contract selects Provider budgets whose sum exceeds 500
- **THEN** policy validation rejects it before activation instead of silently truncating a Provider at request time

#### Scenario: Candidate becomes unreadable before ranking
- **WHEN** a recalled candidate is private, deleted, down, non-published, or media-unready at visibility revalidation
- **THEN** it is removed before feature loading and final ranking without allowing another recency-only reduction

#### Scenario: All healthy Providers return within bounds
- **WHEN** selected Providers complete successfully and their unique merged output is within the validated budget sum
- **THEN** Provider completion order and map iteration order do not change which candidates reach the Ranker

#### Scenario: Legacy no-policy caller requests a bounded pool
- **WHEN** a direct compatibility caller invokes recall without a versioned policy
- **THEN** Frux preserves its existing deterministic bounded result and exposure behavior without applying the policy-driven complete-pool contract
