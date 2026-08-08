## MODIFIED Requirements

### Requirement: Redesigned shell browser coverage
The browser smoke workflow SHALL verify the redesigned application shell at wide, compact, and narrow desktop widths using stable `data-ui` markers, element geometry, and horizontal-overflow checks.

#### Scenario: Wide desktop shell geometry
- **WHEN** the smoke workflow opens a user route at 1440px viewport width
- **THEN** the side navigation, top header, main content, and active route marker are visible, the side navigation is 160px wide, the header is 56px high, and the document has no horizontal overflow

#### Scenario: Compact desktop shell geometry
- **WHEN** the smoke workflow opens a user route at 1200px viewport width
- **THEN** the side navigation is 72px wide, the header remains 56px high, search and primary actions remain reachable, and the document has no horizontal overflow

#### Scenario: Compact boundary remains stable
- **WHEN** the smoke workflow opens a user route at 1024px viewport width
- **THEN** compact navigation, clamped search, compact identity or login, and any overflow action remain visible and operable without overlapping one another

#### Scenario: Narrow desktop shell geometry
- **WHEN** the smoke workflow opens a user route at 800px viewport width
- **THEN** the 72px desktop side navigation and top header remain visible, no mobile bottom navigation is rendered, primary actions remain reachable, and the document has no horizontal overflow

### Requirement: Redesigned Feed panel coverage
The browser smoke workflow SHALL verify the Feed stage, compact overlay density, playback controls, and threaded comment surface before and after opening comments at wide, compact, and narrow desktop widths.

#### Scenario: Desktop comments reduce player width
- **WHEN** the smoke workflow records the Feed-stage width, opens comments at 1440px, and records the width again
- **THEN** the 346px details panel is visible, the player width is smaller than its closed state, and the action rail remains inside the player column

#### Scenario: Desktop thread interaction remains visible
- **WHEN** the workflow expands replies, selects a reply target, and toggles a comment like on desktop
- **THEN** the thread controls, selected target, composer, and updated like state remain visible without clipping or horizontal overflow

#### Scenario: Compact desktop comments use side drawer
- **WHEN** the smoke workflow opens comments at 1200px or 1024px viewport width
- **THEN** the details panel is presented as a right-side drawer and sorting, expansion, reply composition, and closing remain operable without changing the active Feed item

#### Scenario: Narrow desktop comments use side drawer
- **WHEN** the smoke workflow opens comments at 800px viewport width
- **THEN** the details panel remains a dismissible right-side modal drawer, is clamped to the viewport, and exposes all discussion controls without rendering below the player

#### Scenario: Narrow Feed overlays do not collide
- **WHEN** a playable Feed item renders at 800px viewport width with comments closed
- **THEN** metadata and status content do not overlap the action rail or player controls and the stage creates no horizontal overflow

#### Scenario: Narrow player controls remain operable
- **WHEN** the workflow uses play, mute, continuous play, quality, playback rate, seek, and fullscreen controls at 1024px and 800px viewport widths
- **THEN** every capability remains reachable, visible menus stay within the viewport, and media state updates without an uncaught browser error

#### Scenario: Player controls update media state
- **WHEN** the smoke workflow toggles play, mute, and a valid progress position on a playable Feed item
- **THEN** the media element and visible control states reflect each action without an uncaught browser error

### Requirement: Redesign visual evidence
The browser verification run SHALL capture wide-, compact-, and narrow-desktop screenshots for the shell and Feed, plus representative screenshots for authentication, profile, messages, and upload. Remaining visual differences SHALL be documented rather than silently accepted.

#### Scenario: Shell and Feed viewport matrix is complete
- **WHEN** responsive-layout verification finishes
- **THEN** reviewed screenshots exist at 1440px, 1200px, 1024px, and 800px for the shell and Feed with comments closed, and at least one compact and one narrow screenshot exist with comments open

#### Scenario: Page screenshot set is complete
- **WHEN** redesign verification finishes
- **THEN** wide- and narrow-desktop screenshots exist for authentication, profile, messages, and upload and are reviewed for clipping, overflow, inconsistent density, and unreachable controls
