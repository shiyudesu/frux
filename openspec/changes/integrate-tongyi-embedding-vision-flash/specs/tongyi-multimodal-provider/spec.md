## ADDED Requirements

### Requirement: Explicit supported Tongyi embedding profile
The adapter SHALL select one allowlisted profile from `tongyi-embedding-vision-flash-2026-03-06` and
`tongyi-embedding-vision-flash`, SHALL reject unknown profiles, and SHALL report the exact immutable
Frux contract assigned to the selected model, dimension, resolution, and fusion behavior.

#### Scenario: Adapter reports readiness
- **WHEN** a signed Frux readiness request asks for video or query capability after successful startup probing
- **THEN** the adapter reports both capabilities and the exact selected 768-dimensional contract

#### Scenario: Operator selects the undated model
- **WHEN** adapter and Frux runtime configuration select `tongyi-embedding-vision-flash`
- **THEN** they resolve the same independent-vector/local-fusion contract instead of the dated snapshot contract

#### Scenario: Operator selects an unsupported model
- **WHEN** configuration names a model profile outside the verified allowlist
- **THEN** startup fails before any provider request is accepted

#### Scenario: Repository-local environment file is present
- **WHEN** a native API, Worker, or Adapter process starts inside the repository and discovers `.env.multimodal`
- **THEN** it loads only its allowlisted variables, preserves existing process environment values, and keeps DashScope credentials out of API and Worker

#### Scenario: Repository-local environment file is malformed
- **WHEN** a discovered `.env.multimodal` cannot be parsed
- **THEN** the process fails startup without logging secret values

#### Scenario: Caller requests another contract
- **WHEN** a Frux request carries a different provider, model, revision, dimension, or policy identifier
- **THEN** the adapter returns a signed closed unsupported-contract response without calling DashScope

### Requirement: Native fused video translation
When the dated snapshot profile is selected, the adapter SHALL translate a valid Frux video request into one DashScope input content object whose
`text` is the canonical public video text and whose `multi_images` are ordered Base64 Data URIs for
the prepared cover/keyframes. It SHALL request dense dimension 768 and resolution level 1 and SHALL
NOT send original video URLs, object keys, credentials, user identity, or behavior metadata.

#### Scenario: Video content is embedded
- **WHEN** a valid video request contains canonical text and one or more allowed prepared images
- **THEN** the adapter makes one upstream request and accepts exactly one index-zero `fused` embedding

#### Scenario: Video payload violates a bound
- **WHEN** image count, bytes, pixels, MIME type, digest, Base64 encoding, text, source hash, or request size is invalid
- **THEN** the adapter returns a signed invalid-request response before calling DashScope

### Requirement: Independent-vector video translation
When the undated profile is selected, the adapter SHALL translate a valid Frux video request into one
text content element followed by one `multi_images` content element. It SHALL require the corresponding
independent result types, normalize both vectors, calculate their equal-weight normalized mean, and
bind the result to the `normalized-mean-fusion-v1` contract.

#### Scenario: Undated-model video content is embedded
- **WHEN** a valid video request is handled by the undated profile
- **THEN** the adapter accepts exactly index-zero `text` and index-one `multi_images` embeddings and returns their deterministic normalized mean

#### Scenario: Independent result shape is incompatible
- **WHEN** either result is missing, reordered, mistyped, malformed, zero, or non-finite
- **THEN** the adapter returns a signed terminal invalid response and no vector

### Requirement: Same-space query translation
The adapter SHALL translate a valid Frux query request into one DashScope text content element using
the selected profile's model ID and the same output dimension as its video embeddings.

#### Scenario: Query text is embedded
- **WHEN** the adapter receives a valid canonical public query
- **THEN** it makes one upstream request and accepts exactly one index-zero `text` embedding in the selected model space

### Requirement: Upstream validation and error isolation
The adapter SHALL use a bounded non-redirecting HTTPS client, Bearer authentication, one attempt per
Frux call, bounded response reads, and closed error mapping. It SHALL require the selected profile's
exact valid finite non-zero upstream vector set, normalize the result, calculate its digest, and never expose the API key,
raw upstream body, endpoint, or arbitrary upstream error through the Frux protocol.

#### Scenario: Upstream returns a valid vector
- **WHEN** the selected model returns the expected 768-dimensional finite non-zero embedding result set
- **THEN** the adapter returns its L2-normalized values, Frux digest, source hash, contract, and signed operation identity

#### Scenario: Upstream rate limits or fails temporarily
- **WHEN** DashScope returns HTTP 408, 429, or 5xx, times out, or is unreachable
- **THEN** the adapter returns a signed retryable closed error and preserves a bounded Retry-After value when supplied

#### Scenario: Credentials, model, or request are rejected
- **WHEN** DashScope returns another non-success status
- **THEN** the adapter returns a signed terminal closed error without including the upstream response body

#### Scenario: Upstream response is malformed
- **WHEN** the response has the wrong embedding count, index, type, dimension, non-finite or zero norm values, or exceeds the response limit
- **THEN** the adapter returns a signed terminal invalid response and no vector

### Requirement: Model-backed startup and bounded observability
The adapter process SHALL complete a bounded real text-embedding probe before listening, expose an
unauthenticated liveness endpoint without model or credential details, and expose fixed-cardinality
operation/result metrics plus aggregate input-token counters.

#### Scenario: Startup probe succeeds
- **WHEN** configured credentials and the shared public DashScope endpoint return a valid vector from the selected model
- **THEN** the HTTP server starts and signed readiness becomes available

#### Scenario: Startup probe fails
- **WHEN** credentials, endpoint, model availability, response validation, or deadline fails
- **THEN** the process exits without becoming ready

#### Scenario: Usage is observed
- **WHEN** an upstream request succeeds with valid usage metadata
- **THEN** aggregate input, image, and text token counters are updated using only fixed operation labels
