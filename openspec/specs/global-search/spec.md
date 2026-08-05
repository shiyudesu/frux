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
Video search SHALL return only published, public, media-ready videos whose title or description matches the normalized query. Results SHALL use deterministic relevance followed by `published_at DESC, id DESC`, and cursors SHALL be bound to the video result type and normalized query.

#### Scenario: Video title matches exactly
- **WHEN** a readable video's title equals the normalized query ignoring case
- **THEN** it ranks ahead of title-prefix, title-contains, and description-only matches

#### Scenario: Matching video is private or unavailable
- **WHEN** a matching video is private, deleted, down, or not media-ready
- **THEN** the video and its metadata are absent from search results

#### Scenario: Video search continues from a cursor
- **WHEN** the caller submits a valid video cursor for the same normalized query
- **THEN** the next stable page is returned without duplicates or gaps across equal relevance and timestamps

### Requirement: Active user search
User search SHALL return only active accounts whose public account identifier or nickname matches the normalized query. Results SHALL expose only public profile fields and SHALL use deterministic relevance followed by `updated_at DESC, id DESC`.

#### Scenario: Account identifier matches exactly
- **WHEN** an active user's normalized account equals the query
- **THEN** that user ranks ahead of prefix and contains matches

#### Scenario: Matching account is frozen or canceled
- **WHEN** a non-active account matches the query
- **THEN** it is omitted and no private account status or role is exposed

#### Scenario: Visitor opens a user result
- **WHEN** a user search result is selected
- **THEN** the Web navigates to the existing typed public-profile route for that user

### Requirement: Query-bound cursor safety
Search cursors SHALL be opaque, versioned, and bound to the normalized query and result category.

#### Scenario: Cursor is reused with another query
- **WHEN** a caller submits a cursor created for a different query or search category
- **THEN** the API rejects the cursor as invalid instead of returning a mixed page

### Requirement: Search Web experience
The Web SHALL provide a typed `/search` route with validated `q` and `tab` parameters, independent video and user result states, and truthful prompt, loading, error, empty, ready, and loading-more presentation.

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
