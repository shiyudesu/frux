## ADDED Requirements

### Requirement: Registered Rate-Limit Policies
Protected endpoint groups SHALL reference validated registered policies defining identity dimension, local capacity, refill rate, distributed coordination, fallback mode, and retry metadata.

#### Scenario: Route references an unknown policy
- **WHEN** router composition references a policy name that is not registered
- **THEN** startup fails rather than serving the route without its intended protection

#### Scenario: Policy contains invalid bounds
- **WHEN** capacity, refill, timeout, or fallback configuration is outside documented bounds
- **THEN** configuration loading fails with an explicit error

### Requirement: Local-First Enforcement
Every protected request SHALL pass a bounded in-process token bucket before any optional distributed quota operation.

#### Scenario: Local bucket rejects a request
- **WHEN** the request identity has exhausted its local tokens
- **THEN** Frux returns HTTP 429 without calling Redis or the endpoint handler

#### Scenario: Local entry map is full
- **WHEN** a new untrusted identity arrives while no expired entry can be reclaimed
- **THEN** Frux rejects or conservatively limits the identity without growing memory beyond the configured bound

#### Scenario: Saturated map contains expired identities
- **WHEN** a new identity arrives at capacity
- **THEN** Frux uses bounded indexed expiry reclamation rather than scanning the full entry map under the global lock

### Requirement: Bounded Distributed Coordination
Policies requiring cross-instance quotas SHALL use one atomic Redis operation with a bounded deadline and an explicit non-unlimited fallback.

#### Scenario: Redis response is delayed or ambiguous
- **WHEN** the policy context deadline expires during the mutating Lua operation
- **THEN** the Redis client stops on that deadline and does not retry the mutation for the request

#### Scenario: Redis grants distributed capacity
- **WHEN** the local guard passes and distributed tokens remain
- **THEN** the request proceeds and both layers record an allow result

#### Scenario: Redis is unavailable for public read policy
- **WHEN** distributed coordination fails for a policy configured with local fallback
- **THEN** the stricter local fallback decides the request and Frux records a fallback result

#### Scenario: Redis is unavailable for fail-closed policy
- **WHEN** distributed coordination fails for an explicitly fail-closed endpoint group
- **THEN** Frux rejects the request with the policy's stable availability error

### Requirement: Trusted Rate-Limit Identity
Rate-limit identities SHALL derive from authenticated user ID or trusted proxy-normalized client IP and SHALL NOT accept arbitrary client-provided descriptors.

#### Scenario: Authenticated user calls protected write
- **WHEN** a valid session user invokes a user-scoped policy
- **THEN** the quota key uses the server-derived user ID

#### Scenario: Untrusted forwarded header is supplied
- **WHEN** a direct caller sends a spoofed forwarding header outside the trusted proxy boundary
- **THEN** Frux ignores it when deriving the client IP quota key

### Requirement: Stable Rejection and Metrics
Rate-limited requests SHALL return HTTP 429 with stable error code and retry metadata, and metrics SHALL use only registered endpoint group, layer, and result labels.

#### Scenario: Request exceeds quota
- **WHEN** any enforcement layer rejects a request
- **THEN** the response identifies the stable rate-limit condition without exposing internal keys or Redis data

### Requirement: Playback Limiter Compatibility
Playback telemetry SHALL migrate to the shared limiter while preserving the legacy maximum N batches in each fixed 60-second window.

#### Scenario: Existing telemetry user reaches the configured limit
- **WHEN** the user submits the same number of batches previously accepted by the dedicated limiter
- **THEN** the shared policy accepts those batches and rejects the next one consistently

#### Scenario: Time advances within a telemetry window
- **WHEN** a user exhausts the telemetry quota and less than 60 seconds pass
- **THEN** no capacity refills before the fixed-window reset boundary
