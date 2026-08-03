## MODIFIED Requirements

### Requirement: Feed and interaction coverage
The smoke test SHALL visit timeline, recommendation, following, and hot feed scenes and SHALL exercise available like, favorite, follow, navigation, swipe, root-comment, reply, comment-like, expansion, sorting, pagination, and permitted deletion interactions without runtime failures.

#### Scenario: All feed scenes render
- **WHEN** the test visits each of the four feed routes
- **THEN** every route renders its expected feed state without an application crash

#### Scenario: Feed interaction persists
- **WHEN** the test performs an available like, favorite, follow, root-comment, reply, or comment-like action
- **THEN** the interface reflects the successful action and the corresponding API request succeeds exactly once

#### Scenario: Thread pagination remains coherent
- **WHEN** the test switches root sorting, loads another root page, expands a thread, and loads another reply page
- **THEN** existing items remain deduplicated, ordering matches the selected mode, and counters remain consistent

#### Scenario: Comment deletion follows actor permissions
- **WHEN** the test exercises commenter self-deletion and video-author moderation
- **THEN** self-deleted roots with replies retain a tombstone while moderated roots hide their complete thread

### Requirement: Redesigned Feed panel coverage
The browser smoke workflow SHALL verify the Feed stage and threaded comment surface before and after opening comments at desktop and mobile widths.

#### Scenario: Desktop comments reduce player width
- **WHEN** the smoke workflow records the Feed-stage width, opens comments at 1440px, and records the width again
- **THEN** the 346px details panel is visible, the player width is smaller than its closed state, and the action rail remains inside the player column

#### Scenario: Desktop thread interaction remains visible
- **WHEN** the workflow expands replies, selects a reply target, and toggles a comment like on desktop
- **THEN** the thread controls, selected target, composer, and updated like state remain visible without clipping or horizontal overflow

#### Scenario: Mobile comments use bottom sheet
- **WHEN** the smoke workflow opens comments at 390px viewport width
- **THEN** the details panel is presented as a bottom sheet and sorting, expansion, reply composition, and closing remain operable without changing the active Feed item

#### Scenario: Player controls update media state
- **WHEN** the smoke workflow toggles play, mute, and a valid progress position on a playable Feed item
- **THEN** the media element and visible control states reflect each action without an uncaught browser error

## ADDED Requirements

### Requirement: Actionable comment notification coverage
The browser smoke workflow SHALL verify that reply and comment-like messages navigate to their structured discussion targets.

#### Scenario: Reply message opens its thread
- **WHEN** the test user receives and activates a reply notification
- **THEN** the message becomes read, the typed video-detail route opens, the root thread is expanded, and the target reply is highlighted

#### Scenario: Removed target fails safely
- **WHEN** the workflow activates a message whose target discussion is no longer readable
- **THEN** the application displays the unavailable-discussion state without a console error, failed route, or leaked comment content
