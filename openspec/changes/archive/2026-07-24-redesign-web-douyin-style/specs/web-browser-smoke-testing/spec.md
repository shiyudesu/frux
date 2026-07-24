## ADDED Requirements

### Requirement: Redesigned shell browser coverage
The browser smoke workflow SHALL verify the redesigned application shell at wide desktop and mobile widths using stable `data-ui` markers and element geometry.

#### Scenario: Wide desktop shell geometry
- **WHEN** the smoke workflow opens a user route at 1440px viewport width
- **THEN** the side navigation, top header, main content, and active route marker are visible, the side navigation is 160px wide, the header is 56px high, and the document has no horizontal overflow

#### Scenario: Mobile shell geometry
- **WHEN** the smoke workflow opens a user route at a viewport width of 390px
- **THEN** the desktop side navigation is hidden, the mobile navigation is visible, primary touch controls meet the required target size, and the document has no horizontal overflow

### Requirement: Redesigned Feed panel coverage
The browser smoke workflow SHALL verify the Feed stage before and after opening comments at desktop and mobile widths.

#### Scenario: Desktop comments reduce player width
- **WHEN** the smoke workflow records the Feed-stage width, opens comments at 1440px, and records the width again
- **THEN** the 346px details panel is visible, the player width is smaller than its closed state, and the action rail remains inside the player column

#### Scenario: Mobile comments use bottom sheet
- **WHEN** the smoke workflow opens comments at 390px viewport width
- **THEN** the details panel is presented as a bottom sheet and can be closed without changing the active Feed item

#### Scenario: Player controls update media state
- **WHEN** the smoke workflow toggles play, mute, and a valid progress position on a playable Feed item
- **THEN** the media element and visible control states reflect each action without an uncaught browser error

### Requirement: Redesigned page-state coverage
The browser smoke workflow SHALL capture and verify the primary ready state plus relevant loading, error, or empty states for authentication, profiles, messages, upload, relations, and work viewing under the shared visual system.

#### Scenario: User pages share presentation markers
- **WHEN** the smoke workflow visits authentication, messages, own profile, public profile, and upload routes
- **THEN** each route exposes its expected stable `data-ui` markers and remains inside the appropriate redesigned shell or authentication backdrop

#### Scenario: Overlays remain dismissible
- **WHEN** the smoke workflow opens profile editing, relations, work viewing, or mobile comments
- **THEN** each overlay has an accessible close action, traps no unreachable content, and returns focus to a meaningful control when closed

### Requirement: Redesign visual evidence
The browser verification run SHALL capture desktop and mobile screenshots for the shell, Feed with comments closed, Feed with comments open, authentication, profile, messages, and upload. Remaining visual differences SHALL be documented rather than silently accepted.

#### Scenario: Screenshot set is complete
- **WHEN** redesign verification finishes
- **THEN** the required desktop and mobile screenshots exist for all specified routes and states and are reviewed for layout, clipping, overflow, and inconsistent tokens
