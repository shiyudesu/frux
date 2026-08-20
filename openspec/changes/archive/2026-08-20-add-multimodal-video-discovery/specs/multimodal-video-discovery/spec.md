## ADDED Requirements

### Requirement: Exact active-contract semantic retrieval
Frux SHALL provide bounded exact cosine retrieval over only the active multimodal contract and
videos that are currently published, public, media-ready, source-current, and projection-current.
This change MUST NOT require or create an approximate-nearest-neighbor index.

#### Scenario: Exact query returns nearest readable videos
- **WHEN** a valid normalized query vector and bounded limit are submitted
- **THEN** Frux compares every eligible active-contract projection and returns the exact top results by cosine similarity with deterministic tie-breaking

#### Scenario: Projection contains stale content
- **WHEN** a projected row no longer matches authoritative source, contract, visibility, publication, or media-ready facts
- **THEN** it is excluded from the response and scheduled or counted for reconciliation

#### Scenario: No compatible vectors exist
- **WHEN** no readable video has the active contract vector
- **THEN** semantic retrieval returns a healthy empty result rather than failing unrelated lexical or Feed paths

### Requirement: Bounded semantic query embedding
Public video search SHALL obtain a semantic query vector only through the active provider contract,
with normalized-query/model cache identity, fixed input limits, a bounded deadline, a non-blocking
admission bound, and no retry loop inside the HTTP request. Failure to obtain a query vector SHALL
degrade the first page to lexical search.

#### Scenario: Cached query vector is available
- **WHEN** the normalized query and active contract match a valid cached vector
- **THEN** semantic retrieval reuses it without another provider call

#### Scenario: Query embedding succeeds within bounds
- **WHEN** no cached vector exists and the provider returns a valid compatible vector before the deadline
- **THEN** Frux may cache the vector under the normalized query and complete hybrid retrieval

#### Scenario: Query embedding is unavailable
- **WHEN** the provider is disabled, saturated, times out, fails, or returns an invalid vector on a first-page request
- **THEN** video search returns the existing lexical result with an internal degraded observation and no semantic claim

### Requirement: Deterministic lexical and semantic hybrid search
When a compatible semantic query vector is available, public video search SHALL combine bounded
lexical and exact semantic candidates using a versioned deterministic merge and scoring rule,
deduplicate by video ID while retaining both reasons, revalidate readability, and return a stable
query-bound cursor. User search SHALL remain lexical-only.

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

### Requirement: Similar-video discovery
Frux SHALL expose a bounded similar-video query for a readable source video with an active
multimodal vector. Results SHALL use exact cosine similarity, exclude the source itself, revalidate
readability and contract/source equality, use deterministic tie-breaking, and return an opaque
source/model-bound cursor.

#### Scenario: Source video has an active vector
- **WHEN** a caller requests similar videos for a readable source with the active contract vector
- **THEN** Frux returns the nearest other readable videos in stable exact-similarity order

#### Scenario: Source video lacks a compatible vector
- **WHEN** the source is readable but has no current active-contract vector
- **THEN** the API returns a typed semantic-unavailable or healthy-empty result without exposing internal model data

#### Scenario: Source video becomes unreadable
- **WHEN** the source becomes private, deleted, down, non-published, or media-unready
- **THEN** the similar-video API no longer exposes the source or its neighbor relationships

#### Scenario: Similar-video cursor is rebound
- **WHEN** a cursor is reused for another source video, model contract, or result category
- **THEN** Frux rejects it as invalid

### Requirement: Discovery evidence and quality gates
Frux SHALL expose fixed-cardinality operational metrics and reproducible evaluation inputs for
semantic coverage, exact-query latency, query-embedding outcomes, lexical/semantic overlap,
semantic-only contribution, unreadable-result filtering, and similar-video emptiness. Enabling
hybrid search beyond development fixtures SHALL require an accepted human golden set comparing
lexical, text-only where available, image-only where available, and multimodal relevance.

#### Scenario: Hybrid search is evaluated
- **WHEN** operators run the versioned golden-set evaluation
- **THEN** the report records denominators, model/merge versions, Recall/NDCG-style relevance metrics, lexical overlap, and latency without claiming online causal lift

#### Scenario: Metrics are emitted
- **WHEN** semantic discovery succeeds, degrades, returns empty, filters stale rows, or fails
- **THEN** metrics use fixed result/mode/reason labels and exclude query text, vectors, video IDs, user IDs, and raw errors
