## ADDED Requirements

### Requirement: Provider-Agnostic Embedding Port
Frux SHALL expose semantic generation to Go application code only through a narrow provider-neutral
interface that accepts bounded `semantic-text-v1` canonical texts and returns an ordered batch plus
the fixed contract identity. Provider SDK, HTTP, authentication, and response types MUST remain in
infrastructure. The adapter SHALL perform one provider attempt and MUST NOT hide retries.

#### Scenario: A durable caller requests embeddings
- **WHEN** a claimed semantic job or resumable backfill passes canonical texts to the port
- **THEN** the configured adapter performs at most one bounded provider request and returns neutral validated results or a bounded error class

#### Scenario: A synchronous path attempts inference
- **WHEN** API, publication, Feed, ranking, profile, or Kafka-handler code would call the port inline
- **THEN** the architecture and tests reject that dependency because provider invocation is asynchronous-only

### Requirement: Fixed Provider Model Contract
One deployment SHALL pin exactly one provider, model, immutable revision or snapshot, output
dimension, and canonicalizer `semantic-text-v1`. Requests MUST NOT select or override those fields.
Every result SHALL carry the complete tuple. Changing any tuple field SHALL create a new semantic
identity and SHALL require complete rebuild/backfill and explicit cutover rather than in-place
reinterpretation.

#### Scenario: Configuration is valid
- **WHEN** startup loads bounded provider/model/revision/dimension/canonicalizer configuration
- **THEN** every request and result is bound to that exact tuple

#### Scenario: A caller requests another model
- **WHEN** input attempts to name a provider, model, revision, dimension, or canonicalizer
- **THEN** the request is rejected before any provider call

#### Scenario: The pinned contract changes
- **WHEN** an operator changes any identity field
- **THEN** existing rows remain under the old identity and the new identity cannot become active until rebuild coverage is accepted

### Requirement: Independent Semantic Text Canonicalization
`semantic-text-v1` SHALL normalize title and description separately using Unicode NFKC, trim edges,
collapse Unicode-whitespace runs to one ASCII space, reject control/surrogate/invalid scalar values,
enforce 1–200 title and 0–2,000 description code points, and compose title alone or
`title + "\n" + description`. Its lowercase text hash SHALL be
SHA-256 of UTF-8 `"semantic-text-v1\n" + canonical_text`. Provider tokenization MUST NOT alter this
canonical value or hash.

#### Scenario: Equivalent public text is canonicalized
- **WHEN** title/description differ only by compatible Unicode form or whitespace runs
- **THEN** they produce identical canonical text and text hash

#### Scenario: Text is invalid or out of bounds
- **WHEN** normalized fields contain prohibited code points or exceed a bound
- **THEN** canonicalization fails deterministically before cache lookup or provider access

### Requirement: Minimal Public-Text Data Boundary
Only canonical title and description of a video currently established as published and public SHALL
be sent to the provider. Provider payloads MUST NOT contain user IDs, video/business IDs, request or
trace IDs, behavior/interaction data, credentials or tokens, URLs, object-store locations,
private/unpublished drafts, or arbitrary metadata. Results SHALL be correlated by local batch
position rather than outbound business identifiers.

#### Scenario: An eligible video is processed
- **WHEN** a durable caller revalidates that the video is published and public
- **THEN** the provider receives only its canonical text strings and fixed provider model selection

#### Scenario: A video is private or a draft
- **WHEN** eligibility is not currently published and public
- **THEN** no provider request is made

#### Scenario: Logs or payload capture are inspected
- **WHEN** provider work succeeds or fails
- **THEN** no ID, raw/canonical text, hash, URL, behavior data, credential, token, or provider payload appears

### Requirement: Secret-Only Credential Handling
Provider credentials SHALL be loaded only through approved secret/config injection. Credentials,
authorization headers, and derived secret values MUST NOT be persisted in PostgreSQL, Redis, Kafka,
vector caches, jobs, checkpoints, logs, traces, metrics, or error messages. SDK request/response
debug logging SHALL be disabled.

#### Scenario: Credential configuration is missing
- **WHEN** provider execution is enabled without a valid injected credential
- **THEN** the provider gate remains closed and only a bounded configuration class is reported

#### Scenario: Durable state is inspected
- **WHEN** jobs, vectors, cache entries, checkpoints, or Kafka records are read
- **THEN** none contains provider credentials or authorization material

### Requirement: Contract-Scoped Text Hash Deduplication and Cache
The service SHALL deduplicate by
`(provider, model, revision, dimension, canonicalizer, text_hash)` before provider access. A vector
cache MAY store only that complete identity, text hash, validated vector, and bounded timestamps; it
MUST NOT store canonical/raw text or credentials. Cache results SHALL pass the same validation as
provider responses.

#### Scenario: Duplicate canonical texts occur
- **WHEN** multiple items share the same complete identity and text hash
- **THEN** at most one provider item is billed and the validated vector is expanded back to each local position

#### Scenario: A validated cache entry exists
- **WHEN** the exact key is found
- **THEN** the provider is not called for that text

#### Scenario: A cache entry is corrupt or mismatched
- **WHEN** dimension, values, norm, or identity validation fails
- **THEN** the entry is ignored and reported without allowing invalid persistence

### Requirement: Strict Atomic Response Validation
The adapter SHALL enforce bounded response size and require exactly one ordered result per unique
input under the configured contract. Every vector SHALL have the exact configured dimension,
contain only finite components, have a positive norm, be deterministically L2-normalized, and verify
unit norm within `1e-5`. A partial, missing, extra, reordered, malformed, zero, non-finite,
wrong-dimension, or wrong-contract response SHALL reject the complete batch.

#### Scenario: Provider output is valid
- **WHEN** every ordered item has the exact dimension and finite positive-norm components
- **THEN** deterministic L2-normalized vectors may enter cache or downstream persistence

#### Scenario: One output is invalid
- **WHEN** any response item violates identity, order, count, dimension, finiteness, norm, or contract
- **THEN** no vector from that provider response is accepted

### Requirement: Bounded Transport Rate and Retry Classification
Provider access SHALL use bounded request/response sizes, timeout, batch size, in-flight requests,
QPS, and burst. The adapter SHALL preserve only a parsed bounded `Retry-After`. Network errors,
timeouts, `429`, and bounded `5xx` SHALL be retryable by durable callers. Invalid input,
authentication/authorization, unknown model/revision, contract mismatch, malformed response, and
local configuration SHALL be terminal until operator action.

#### Scenario: Provider throttles a request
- **WHEN** the provider returns `429` with a valid `Retry-After`
- **THEN** the adapter returns a retryable rate-limit class and bounded delay without exposing response content

#### Scenario: A request times out
- **WHEN** the end-to-end provider deadline expires
- **THEN** the request is canceled, capacity is released, and the durable caller decides whether and when to retry

#### Scenario: Authentication or contract fails
- **WHEN** the provider rejects credentials or returns an incompatible model response
- **THEN** the result is terminal/operator-actionable and no vector is accepted

### Requirement: Circuit Gate and Failure Isolation
Each process SHALL maintain a bounded local provider circuit/gate. It SHALL open after configured
retryable-failure thresholds and immediately on authentication, quota exhaustion, or contract
mismatch; half-open probes SHALL be rate-limited. An open gate MUST affect only asynchronous
semantic work and MUST NOT block API/Feed startup, publication, Kafka handoff, hash generation, or
`hash-ngram-v1` use.

#### Scenario: Repeated provider failures occur
- **WHEN** the rolling failure threshold is reached
- **THEN** the gate fails semantic calls fast until a bounded half-open probe succeeds

#### Scenario: Provider service is unavailable
- **WHEN** the gate is open
- **THEN** durable semantic work remains retryable while publish, Feed, and hash behavior continue

### Requirement: Cost Quota and Privacy-Safe Observability
Metrics SHALL cover provider calls, unique texts, cache outcomes, duration, bounded result classes,
rate limiting, bounded `Retry-After`, circuit state, input code points, billable units, estimated
cost, actual reported usage when available, quota remaining, and budget-gate pauses. Pricing SHALL
be bound to a configured revision. Labels and logs MUST NOT contain arbitrary provider/model
strings, IDs, text, hashes, URLs, credentials, payloads, raw errors, or retry numbers.

#### Scenario: Usage is estimated
- **WHEN** a batch is prepared
- **THEN** the local pricing estimator reports bounded billable units and estimated cost before authorization to call

#### Scenario: Pricing is missing or stale
- **WHEN** cost-authorized backfill would run without an accepted pricing revision
- **THEN** the budget gate remains closed without affecting hash fallback or live publication

### Requirement: Permanent Hash Fallback and Rebuild Boundary
`hash-ngram-v1` SHALL remain a permanent independent fallback. This capability SHALL NOT host or
train a model, add Python/PyTorch/model artifacts, invoke providers on synchronous request paths,
or add live job/backfill/retrieval/ranking/profile behavior. Future provider contract changes SHALL
be rebuilt under a new identity before explicit cutover.

#### Scenario: Semantic generation is unavailable
- **WHEN** provider calls are disabled, throttled, failing, or over budget
- **THEN** `hash-ngram-v1` remains available and existing product behavior does not wait for semantic output

#### Scenario: Implementation is validated
- **WHEN** tests and `openspec validate --all --strict` run
- **THEN** no local model runtime, Python/PyTorch dependency, model container, or online inference path is introduced

### Requirement: Recommendation Roadmap Gate
This capability SHALL be implemented only after
`persist-recommendation-training-impressions`, `export-recommendation-training-dataset`,
`evaluate-recommendation-policies-offline`, and `learn-recommendation-policy-weights` are completed,
archived, and have met their acceptance gates.

#### Scenario: A prerequisite remains active
- **WHEN** any required recommendation change is incomplete or unaccepted
- **THEN** implementation of this change does not begin
