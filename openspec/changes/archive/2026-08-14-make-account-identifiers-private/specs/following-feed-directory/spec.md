## MODIFIED Requirements

### Requirement: Searchable relationship pagination
`GET /api/users/me/following` SHALL support optional normalized nickname search while preserving cursor pagination. Following and follower responses SHALL omit account identifiers and SHALL continue returning only active relationships to normal target accounts.

#### Scenario: Empty query lists recent follows
- **WHEN** the request omits `q` or supplies only whitespace
- **THEN** active follows are returned in the existing stable recent-follow order without account identifiers

#### Scenario: Query matches nickname
- **WHEN** a valid `q` is supplied
- **THEN** only active followed users whose nickname contains the normalized query are returned

#### Scenario: Query matches only account identifier
- **WHEN** a query matches a followed user's account identifier but not their nickname
- **THEN** that user is not returned because account identifiers are not a relationship discovery key

#### Scenario: Search cursor is reused with another query
- **WHEN** a cursor produced for one normalized query or list kind is supplied with another
- **THEN** the API rejects it as an invalid relation cursor

#### Scenario: Pre-privacy query cursor is used
- **WHEN** a versioned cursor produced under account-or-nickname search rules is supplied with a non-empty query
- **THEN** the API rejects it rather than continuing with incompatible matching semantics

#### Scenario: Legacy cursor is used without search
- **WHEN** an existing compatible cursor is supplied with an empty query
- **THEN** the API may continue the unfiltered list without changing ordering

#### Scenario: Invalid search query is supplied
- **WHEN** `q` exceeds the supported Unicode length or otherwise fails validation
- **THEN** the API returns the stable relation validation error and issues no unbounded database query

### Requirement: Truthful people rows
Directory rows SHALL display only available relationship and public profile facts: avatar fallback, nickname, and optional bio. The directory MUST NOT display account identifiers, live state, unread-work counts, or activity badges without corresponding public Frux data.

#### Scenario: Followed user has complete public profile data
- **WHEN** a directory row has nickname, avatar, and bio
- **THEN** those values are displayed using the shared public identity and avatar rules without an account identifier

#### Scenario: Followed user lacks optional profile data
- **WHEN** avatar or bio is empty
- **THEN** the shared default avatar or a neutral empty secondary presentation is used without invented text or counts

#### Scenario: Relationship response contains user identity
- **WHEN** the Web receives a following or follower row
- **THEN** the row contains no account identifier to render, cache, or use for local filtering
