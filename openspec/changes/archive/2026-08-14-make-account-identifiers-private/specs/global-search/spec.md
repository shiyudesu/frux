## MODIFIED Requirements

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
