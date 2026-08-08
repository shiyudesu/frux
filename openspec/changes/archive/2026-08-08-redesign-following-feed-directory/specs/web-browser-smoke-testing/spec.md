## ADDED Requirements

### Requirement: Following Feed directory browser coverage
The browser smoke workflow SHALL verify the authenticated Following directory, video stage, search, pagination, profile navigation, collapse behavior, scroll isolation, and comment integration.

#### Scenario: Wide Following geometry
- **WHEN** the workflow opens Following at 1440px with the directory visible
- **THEN** the main rail is 160px, the directory is 208px, the stage remains visible, and the document has no horizontal overflow

#### Scenario: Compact Following geometry
- **WHEN** the workflow opens Following at 1200px or 1024px
- **THEN** the main rail is 72px, the directory remains 208px, and essential stage controls remain reachable

#### Scenario: Narrow Following geometry
- **WHEN** the workflow opens Following at 800px
- **THEN** the main rail, 208px directory, stage, directory controls, and essential Feed controls remain reachable without mobile navigation

#### Scenario: Directory collapse expands the stage
- **WHEN** the workflow records stage width, collapses the directory, and records width again
- **THEN** stage width increases by the released directory space and the active video ID remains unchanged

#### Scenario: Directory scrolling does not navigate Feed
- **WHEN** the workflow scrolls the directory while recording the active video ID
- **THEN** more relationship rows become visible and the active video ID does not change

#### Scenario: Following search uses complete server results
- **WHEN** the workflow searches for a followed account not present in the initial page
- **THEN** the matching relationship is returned through the search API and displayed without appending stale previous-query rows

#### Scenario: Directory row opens public profile
- **WHEN** the workflow activates a followed-person row
- **THEN** the typed public-profile route opens and browser Back returns to an unfiltered Following Feed

#### Scenario: Following comments remain operable
- **WHEN** comments are opened with the directory visible at 1440px, 1280px, 1200px, and 800px
- **THEN** each viewport uses its specified push, temporary-collapse, or drawer presentation without changing the active video

#### Scenario: Unsupported activity facts are absent
- **WHEN** the directory renders ordinary Follow relationships
- **THEN** no live, unread-work, or guessed activity badge is displayed without corresponding API data

## MODIFIED Requirements

### Requirement: Redesign visual evidence
The browser verification run SHALL capture wide-, compact-, and narrow-desktop screenshots for the shell, standard Feed, and Following Feed directory, plus representative screenshots for authentication, profile, messages, and upload. Remaining visual differences SHALL be documented rather than silently accepted.

#### Scenario: Shell and Feed viewport matrix is complete
- **WHEN** responsive-layout verification finishes
- **THEN** reviewed screenshots exist at 1440px, 1200px, 1024px, and 800px for the standard Feed and Following Feed with the directory open, plus collapsed-directory and comments-open states

#### Scenario: Page screenshot set is complete
- **WHEN** redesign verification finishes
- **THEN** wide- and narrow-desktop screenshots exist for authentication, profile, messages, and upload and are reviewed for clipping, overflow, inconsistent density, and unreachable controls
