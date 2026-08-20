# global-search Specification

## Purpose

Defines validated public video and user search, deterministic relevance and cursor safety, and the typed Web search experience.

## Requirements

### Requirement: Validated public search queries
Frux SHALL provide independently paginated public video and user search APIs. Search queries SHALL be trimmed, SHALL contain 1-64 Unicode code points, SHALL escape wildcard metacharacters, and SHALL use limits from 1-50.

#### Scenario: Caller submits a valid query
- **WHEN** a caller searches videos or users with a valid query and limit
- **THEN** the API returns typed `items`, `next_cursor`, and `has_more` fields without requiring authentication

#### Scenario: Caller submits an empty or oversized query
- **WHEN** the trimmed query is empty or exceeds 64 Unicode code points
- **THEN** the API returns a validation error and performs no unbounded search

#### Scenario: Query contains wildcard characters
- **WHEN** the query contains `\`, `%`, or `_`
- **THEN** those characters are matched literally rather than changing the search predicate

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

### Requirement: Active user search
User search SHALL return only active accounts whose nickname matches the normalized query. Results SHALL expose only the user ID, nickname, avatar, and bio and SHALL use deterministic nickname relevance followed by `updated_at DESC, id DESC`.

#### Scenario: Nickname matches exactly
- **WHEN** an active user's nickname equals the query ignoring case
- **THEN** that user ranks ahead of nickname-prefix and nickname-contains matches

#### Scenario: Query matches only an account identifier
- **WHEN** the query matches an active user's canonical account identifier but not their nickname
- **THEN** that user is absent from the search results

#### Scenario: Matching account is frozen or canceled
- **WHEN** a non-active user's nickname matches the query
- **THEN** the user is omitted and no private account status, role, or account identifier is exposed

#### Scenario: Visitor opens a user result
- **WHEN** a user search result is selected
- **THEN** the Web navigates to the existing typed public-profile route using the user ID without displaying or caching an account identifier

#### Scenario: Legacy user-search cursor is submitted
- **WHEN** the caller submits a user-search cursor created under account-and-nickname relevance rules
- **THEN** the API rejects the cursor as invalid instead of mixing the old and new result sets

### Requirement: Query-bound cursor safety
Search cursors SHALL be opaque, versioned, and bound to the normalized query and result category.

#### Scenario: Cursor is reused with another query
- **WHEN** a caller submits a cursor created for a different query or search category
- **THEN** the API rejects the cursor as invalid instead of returning a mixed page

### Requirement: Search Web experience
The Web SHALL provide a typed `/search` route with validated `q` and `tab` parameters, independent video and user result states, truthful prompt, loading, error, empty, ready, and loading-more presentation, and a height-constrained scroll container that keeps every result and pagination control reachable within the fixed application shell.

#### Scenario: User submits the top navigation search
- **WHEN** the user enters a non-empty query and presses Enter or activates Search
- **THEN** the app navigates to `/search` with an encoded query and displays video results by default

#### Scenario: User switches result categories
- **WHEN** the user switches between Videos and Users
- **THEN** each category retains its own items, cursor, has-more, loading, and error state for the current query

#### Scenario: Search query changes while a request is pending
- **WHEN** an older request resolves after navigation to another query
- **THEN** the stale response cannot populate or append to the new query's results

#### Scenario: User selects a video result
- **WHEN** a readable video result is selected
- **THEN** the app navigates to the existing typed video destination for that video

#### Scenario: Search input is empty
- **WHEN** the normalized route query is empty
- **THEN** the page shows a search prompt and issues no video or user search request

#### Scenario: First page exceeds the viewport
- **WHEN** a video or user search page contains more rows than fit in the available application-body height
- **THEN** the search route scrolls vertically and every returned row remains reachable without document-level scrolling

#### Scenario: More search results are available
- **WHEN** the current search response has `has_more=true` and a non-empty `next_cursor`
- **THEN** the load-more control is reachable by scrolling and requests the next page with that cursor

#### Scenario: Next search page resolves
- **WHEN** the user loads another cursor page successfully
- **THEN** new unique results are appended to the active category and the updated cursor and has-more state control further pagination
