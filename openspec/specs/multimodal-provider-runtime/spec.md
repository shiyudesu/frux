# multimodal-provider-runtime Specification

## Purpose

Defines the secure, bounded, model-neutral HTTP boundary and process composition used to connect
Frux to an external multimodal inference runtime.

## Requirements

### Requirement: Versioned authenticated multimodal provider protocol
Frux SHALL invoke external multimodal inference through a versioned HTTP protocol that defines
readiness, public-video embedding, and public-query embedding operations. Every request and response
SHALL be bound to an operation ID and authenticated over its exact body, and non-loopback endpoints
MUST use HTTPS.

#### Scenario: Video embedding request is sent
- **WHEN** the Worker submits validated canonical public text and prepared bounded images
- **THEN** the provider receives no storage URL, credential, user identity, behavior data, or arbitrary metadata and the request is authenticated with the configured protocol version

#### Scenario: Query embedding request is sent
- **WHEN** the API submits a validated canonical public search query
- **THEN** the provider receives only the query, requested contract, source hash, protocol envelope, and no authenticated-user or request-session identity

#### Scenario: Remote endpoint uses insecure transport
- **WHEN** configuration enables an HTTP endpoint whose host is not loopback
- **THEN** Frux rejects configuration before making a provider request

#### Scenario: Provider redirects a signed request
- **WHEN** an embedding or readiness endpoint returns a redirect
- **THEN** Frux rejects the response and does not forward the signed request to the redirect target

### Requirement: Provider readiness and contract compatibility
An API or Worker process that requires live inference SHALL perform a bounded signed readiness
handshake before enabling the dependent path. The reported protocol, operation capabilities, and
complete immutable multimodal contract SHALL match configuration exactly.

#### Scenario: Compatible provider is ready
- **WHEN** the signed readiness response reports the configured protocol, required operation, and exact contract
- **THEN** the process may construct and expose the dependent multimodal runtime

#### Scenario: Provider contract differs
- **WHEN** any provider, model, revision, dimension, canonicalizer, frame, preprocessing, or fusion field differs
- **THEN** process startup fails before a job is claimed or hybrid query is served

#### Scenario: Similar-video-only mode starts without inference
- **WHEN** only similar-video retrieval is enabled over already persisted compatible vectors
- **THEN** the API does not require a live provider readiness handshake

### Requirement: Bounded transport and closed failure mapping
The provider client SHALL perform exactly one bounded HTTP attempt per application call, SHALL add no
internal work queue or retry loop, SHALL cap encoded requests and response bodies, and SHALL map
failures to closed retryable or terminal classes without exposing response bodies or secrets.

#### Scenario: Provider is saturated or temporarily unavailable
- **WHEN** the provider returns rate-limit or retryable server status, times out, or encounters a transport failure
- **THEN** Frux returns a retryable provider error to the existing bounded Worker or query degradation policy

#### Scenario: Provider rejects valid transport permanently
- **WHEN** the provider returns a non-retryable client or policy rejection
- **THEN** Frux returns a terminal provider error without copying the raw response into normal logs or job state

#### Scenario: Response is untrusted or invalid
- **WHEN** the response signature, operation ID, source hash, contract, vector digest, dimensions, finite components, norm, JSON shape, or size is invalid
- **THEN** Frux rejects the result and persists or caches no vector

#### Scenario: Caller cancels the operation
- **WHEN** the request context is canceled or reaches its deadline
- **THEN** the HTTP request is canceled and the client starts no detached retry or continuation

### Requirement: Runtime composition remains feature-scoped
Frux SHALL construct one reusable provider client per process only when that process has an enabled
inference-dependent feature. Worker video-job execution and API query/hybrid search SHALL receive the
same configured contract, while disabled and similar-only paths SHALL remain provider-independent.

#### Scenario: Video job execution is enabled
- **WHEN** the Worker starts with multimodal video jobs enabled and a compatible provider
- **THEN** it runs the existing fenced multimodal job worker with configured media preparation, admission, deadline, retry, heartbeat, and shutdown bounds

#### Scenario: Hybrid search is enabled
- **WHEN** the API starts with query embedding and hybrid video search enabled and a compatible provider
- **THEN** public video search uses the constructed bounded query embedder and exact semantic index under the configured hybrid rule

#### Scenario: Multimodal runtime is disabled
- **WHEN** all multimodal feature flags are disabled
- **THEN** API and Worker startup require no endpoint, secret, model contract, readiness call, or model runtime

### Requirement: Provider conformance evidence
Frux SHALL include deterministic transport conformance tests covering authenticated request shape,
response validation, compatibility, failure mapping, cancellation, and process wiring. Conformance
fixtures MUST NOT be exposed as a selectable product model or counted as semantic-quality evidence.

#### Scenario: Adapter conformance suite runs
- **WHEN** the provider adapter tests execute against the deterministic conformance server
- **THEN** success and each bounded security, compatibility, transport, and validation failure path produce the specified result without network access to a real model

#### Scenario: Semantic quality is reported
- **WHEN** a user or operator inspects the multimodal evaluation workflow before a concrete model is integrated
- **THEN** documentation states that transport conformance does not demonstrate retrieval relevance or authorize feature activation
