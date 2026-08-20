## MODIFIED Requirements

### Requirement: Public video search
Video search SHALL return only published, public, media-ready videos. It SHALL always preserve the
existing validated lexical title/description retrieval and, when a compatible active multimodal
query vector is available, SHALL combine bounded lexical and exact semantic candidates through a
versioned deterministic hybrid rule. Results SHALL use stable hybrid relevance followed by
`published_at DESC, id DESC`; lexical-only fallback SHALL retain the existing lexical relevance
order. Cursors SHALL be opaque and bound to the video result type, normalized query, retrieval mode,
ranking version, and active model contract when applicable.

#### Scenario: Video title matches exactly
- **WHEN** a readable video's title equals the normalized query ignoring case
- **THEN** its lexical reason ranks ahead of title-prefix, title-contains, and description-only lexical reasons and participates in the active hybrid rule when semantic retrieval is available

#### Scenario: Video is semantically related without a lexical match
- **WHEN** a readable video is returned only by exact active-contract semantic retrieval
- **THEN** it may appear in hybrid video search according to the versioned semantic reservation and score without inventing a lexical match

#### Scenario: Semantic query embedding is unavailable on the first page
- **WHEN** the semantic provider is disabled, saturated, unavailable, times out, or returns an invalid query vector
- **THEN** the API returns the existing lexical-only video result and observes degraded semantic search without failing public search

#### Scenario: Matching video is private or unavailable
- **WHEN** a lexical or semantic candidate is private, deleted, down, non-published, media-unready, source-stale, or projection-stale
- **THEN** the video and its metadata are absent from search results

#### Scenario: Video search continues from a lexical cursor
- **WHEN** the caller submits a valid lexical video cursor for the same normalized query
- **THEN** the next stable lexical page is returned without duplicates or gaps across equal relevance and timestamps

#### Scenario: Video search continues from a hybrid cursor
- **WHEN** the caller submits a valid hybrid cursor for the same normalized query, hybrid version, and active model contract
- **THEN** the next stable hybrid page is returned under the same retrieval mode without silently switching to lexical-only ordering

#### Scenario: Legacy video-search cursor is submitted after hybrid ranking activation
- **WHEN** a cursor lacks the required retrieval-mode or ranking-version binding
- **THEN** the API rejects it as invalid instead of mixing legacy and hybrid result sets
