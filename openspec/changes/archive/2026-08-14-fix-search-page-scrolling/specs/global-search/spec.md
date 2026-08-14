## MODIFIED Requirements

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
