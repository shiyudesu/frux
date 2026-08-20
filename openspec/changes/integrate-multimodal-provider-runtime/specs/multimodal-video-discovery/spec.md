## MODIFIED Requirements

### Requirement: Bounded semantic query embedding
Public video search SHALL obtain a semantic query vector only through the active provider contract,
with normalized-query/model cache identity, fixed input limits, a bounded deadline, a non-blocking
admission bound, and no retry loop inside the HTTP request. An enabled API process SHALL validate the
provider protocol, query capability, and complete contract before serving semantic queries. Failure
to obtain a query vector after startup SHALL degrade the first page to lexical search.

#### Scenario: Cached query vector is available
- **WHEN** the normalized query and active contract match a valid cached vector
- **THEN** semantic retrieval reuses it without another provider call

#### Scenario: Query embedding succeeds within bounds
- **WHEN** no cached vector exists and the provider returns a valid compatible vector before the deadline
- **THEN** Frux may cache the vector under the normalized query and complete hybrid retrieval

#### Scenario: Query embedding is unavailable
- **WHEN** the provider is disabled, saturated, times out, fails, or returns an invalid query vector on a first-page request
- **THEN** video search returns the existing lexical result with an internal degraded observation and no semantic claim

#### Scenario: Enabled query runtime is incompatible at startup
- **WHEN** readiness reports a different protocol, missing query capability, or different immutable model contract
- **THEN** the API fails startup instead of serving a semantic path that cannot reproduce stored video-vector meaning

### Requirement: Deterministic lexical and semantic hybrid search
When a compatible semantic query vector is available, public video search SHALL combine bounded
lexical and exact semantic candidates using a versioned deterministic merge and scoring rule,
deduplicate by video ID while retaining both reasons, revalidate readability, and return a stable
query-bound cursor. User search SHALL remain lexical-only. The API composition root SHALL install the
hybrid service only when query embedding, exact retrieval, and their shared contract are validated.

#### Scenario: Video matches both retrieval paths
- **WHEN** a readable video is returned by lexical and semantic retrieval
- **THEN** the result contains one video whose internal evidence retains both reasons and whose hybrid score follows the active versioned rule

#### Scenario: Video matches only semantic meaning
- **WHEN** a readable video is semantically related but its title and description do not lexically match the normalized query
- **THEN** it may appear in the hybrid video results according to the bounded semantic reservation and score

#### Scenario: Hybrid search continues from a cursor
- **WHEN** the caller submits a valid cursor for the same normalized query, hybrid version, retrieval mode, and model contract
- **THEN** Frux returns the next stable page using the same ordering semantics without switching to lexical-only pagination

#### Scenario: Hybrid continuation cannot reproduce semantic mode
- **WHEN** a hybrid cursor is valid but its query vector or contract can no longer be reproduced within the bounded request
- **THEN** Frux returns a bounded retryable search error rather than mixing lexical-only results into the hybrid cursor sequence

#### Scenario: Hybrid dependencies are incomplete at startup
- **WHEN** hybrid search is enabled without a compatible provider, query cache, exact index, readable-video loader, or valid hybrid configuration
- **THEN** the API fails startup and does not silently expose a partially wired hybrid path
