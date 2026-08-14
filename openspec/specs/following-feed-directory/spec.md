# following-feed-directory Specification

## Purpose

Defines the authenticated Following Feed people directory, its relationship API contract, independent client state, truthful presentation, responsive behavior, and coordination with Feed interactions.

## Requirements

### Requirement: Authenticated Following Feed directory
The authenticated Following Feed SHALL render a people directory beside the independently scrollable video stage. The directory SHALL be visible by default only for the Following scene and SHALL use the current user's active Follow relationships.

#### Scenario: User opens Following Feed
- **WHEN** an authenticated user navigates to the Following scene
- **THEN** the page renders the main navigation, a 208px Following directory, and the ordered Following video stage without horizontal document overflow

#### Scenario: User opens another Feed scene
- **WHEN** the user navigates to Timeline, Recommend, or Hot
- **THEN** the Following directory is absent and those scenes retain their existing stage geometry

#### Scenario: Unauthenticated user opens Following Feed
- **WHEN** the user has no valid session
- **THEN** the existing authentication state is shown and no relationship data request is exposed

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

### Requirement: Independent directory state
The Web client SHALL maintain directory items, query, cursor, loading, loading-more, empty, and error state independently from Following Feed items and cursors.

#### Scenario: Query changes during a request
- **WHEN** an old directory response arrives after the normalized query changed
- **THEN** the old response is ignored and cannot replace or append to the current results

#### Scenario: Directory loads another page
- **WHEN** the user reaches the directory pagination boundary and `has_more` is true
- **THEN** the next cursor page is appended without duplicate users or changes to the active video

#### Scenario: Directory request fails after existing items
- **WHEN** loading another page fails
- **THEN** existing rows remain visible and a reachable retry action is shown

#### Scenario: User follows nobody
- **WHEN** the active following list is empty
- **THEN** the directory shows a truthful empty state while the Following Feed shows its existing empty behavior

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

### Requirement: Profile navigation without Feed filtering
Activating a directory row SHALL open the existing typed public-profile destination. It SHALL NOT replace the Following Feed with an undeclared author-only Feed or mutate the active relationship.

#### Scenario: User activates a directory row
- **WHEN** a followed-person row is clicked or keyboard-activated
- **THEN** the typed public profile route opens for that user

#### Scenario: User returns from the profile
- **WHEN** browser history returns to the Following Feed
- **THEN** no author filter is inferred from the previously selected row

### Requirement: Directory interaction isolation
Directory scrolling, pointer input, search entry, and controls SHALL NOT trigger Feed wheel, swipe, or playback shortcuts.

#### Scenario: User scrolls the people list
- **WHEN** the pointer is over the directory scroll container and the user scrolls
- **THEN** directory rows move while the active Feed index remains unchanged

#### Scenario: User edits the directory search
- **WHEN** focus is inside the search input
- **THEN** typing, Space, and navigation keys are handled as text-entry controls rather than Feed shortcuts

### Requirement: Collapsible responsive directory
The directory SHALL be open by default and SHALL expose accessible collapse and reopen controls. Collapse SHALL change presentation only and SHALL NOT reload or reorder the Following Feed.

#### Scenario: User collapses the directory
- **WHEN** the collapse control is activated
- **THEN** the 208px column is removed, the stage expands into the released width, and a reopen control remains reachable

#### Scenario: User reopens the directory
- **WHEN** the reopen control is activated
- **THEN** the directory returns with its current query, loaded rows, cursor, and scroll state preserved

#### Scenario: Directory is visible at narrow desktop width
- **WHEN** the viewport is 800px wide and the directory is open
- **THEN** the 72px main rail, 208px directory, stage, and essential Feed controls remain reachable without a mobile bottom navigation

### Requirement: Comment-panel width coordination
Following directory presentation SHALL coordinate with the details panel so that comments do not leave the Feed stage below its supported desktop width.

#### Scenario: Comments open at 1440px or wider
- **WHEN** the Following directory is open and comments are opened at a sufficiently wide desktop viewport
- **THEN** the directory, stage, and 346px push-style details panel coexist

#### Scenario: Push comments open between 1280px and 1439px
- **WHEN** comments are opened while the directory is visible in this range
- **THEN** the directory column is temporarily removed or an equivalent width-safe presentation is used until comments close

#### Scenario: Comments open below 1280px
- **WHEN** comments are opened in compact or narrow desktop mode
- **THEN** the existing right-side modal drawer overlays the stage and directory without changing the active video

### Requirement: Relation mutation coherence
Successful Follow relationship changes made from the active video or profile surfaces SHALL update the directory without waiting for a full browser reload.

#### Scenario: User unfollows the current author
- **WHEN** the unfollow request succeeds in the Following Feed
- **THEN** that author is removed from the directory state and cannot reappear from an older directory response

#### Scenario: User follows an author elsewhere
- **WHEN** a successful follow is reflected while the directory is mounted
- **THEN** the directory can refresh or insert the truthful relationship without duplicating the user
