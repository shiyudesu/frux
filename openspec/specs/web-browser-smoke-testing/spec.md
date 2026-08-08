# web-browser-smoke-testing Specification

## Purpose

Defines end-to-end browser smoke testing for the web application, including environment readiness, authentication, feed interactions, page workflows, runtime diagnostics, and migration verification.

## Requirements

### Requirement: Integrated browser test environment

The smoke test SHALL exercise the web application through a real browser against a healthy local API and its required infrastructure services.

#### Scenario: Environment is ready

- **WHEN** browser verification begins
- **THEN** the web application loads successfully and the API health endpoint reports success

### Requirement: Authentication coverage

The smoke test SHALL verify account registration or creation, login, authenticated route access, logout, and subsequent login with an isolated test account.

#### Scenario: User authenticates successfully

- **WHEN** the test user registers or logs in with valid credentials
- **THEN** the application establishes the session and navigates to an authenticated page

#### Scenario: User logs out and returns

- **WHEN** the authenticated user logs out and then logs in again
- **THEN** protected content is hidden after logout and restored after successful login

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

### Requirement: Page workflow coverage

The smoke test SHALL verify messages, own profile, public profile, profile editing, upload, and work-viewer workflows.

#### Scenario: Upload and owned work are accessible

- **WHEN** the test uploads valid media and opens the current user's works
- **THEN** the uploaded work is represented by the application and its viewer can be opened

#### Scenario: Profile workflows operate

- **WHEN** the test edits the current profile and opens an available public profile
- **THEN** both pages show the expected user data and navigation remains functional

#### Scenario: Messages page operates

- **WHEN** the authenticated user opens messages and uses available read controls
- **THEN** the messages page renders and each issued read request succeeds

### Requirement: Runtime failure detection

The smoke test SHALL fail when a required workflow produces an uncaught browser error, a failed required API request, a broken route, or an unexpected user-visible result.

#### Scenario: Diagnostics remain clean

- **WHEN** all required workflows complete
- **THEN** no unexplained console error or failed required network request remains

### Requirement: Verification completion

The TypeScript migration smoke task SHALL be marked complete only after all required browser scenarios pass and the frontend production build succeeds.

#### Scenario: Migration verification is complete

- **WHEN** all browser scenarios pass and `pnpm run build` succeeds
- **THEN** task 6.4 in `migrate-web-to-typescript` is marked complete

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

### Requirement: Actionable comment notification coverage

The browser smoke workflow SHALL verify that reply and comment-like messages navigate to their structured discussion targets.

#### Scenario: Reply message opens its thread

- **WHEN** the test user receives and activates a reply notification
- **THEN** the message becomes read, the typed video-detail route opens, the root thread is expanded, and the target reply is highlighted

#### Scenario: Removed target fails safely

- **WHEN** the workflow activates a message whose target discussion is no longer readable
- **THEN** the application displays the unavailable-discussion state without a console error, failed route, or leaked comment content

### Requirement: Redesigned page-state coverage

The browser smoke workflow SHALL capture and verify the primary ready state plus relevant loading, error, or empty states for authentication, profiles, messages, upload, relations, and work viewing under the shared visual system.

#### Scenario: User pages share presentation markers

- **WHEN** the smoke workflow visits authentication, messages, own profile, public profile, and upload routes
- **THEN** each route exposes its expected stable `data-ui` markers and remains inside the appropriate redesigned shell or authentication backdrop

#### Scenario: Overlays remain dismissible

- **WHEN** the smoke workflow opens profile editing, relations, work viewing, or compact comments
- **THEN** each overlay has an accessible close action, traps no unreachable content, and returns focus to a meaningful control when closed

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

### Requirement: Redesign visual evidence

The browser verification run SHALL capture wide-, compact-, and narrow-desktop screenshots for the shell, standard Feed, and Following Feed directory, plus representative screenshots for authentication, profile, messages, and upload. Remaining visual differences SHALL be documented rather than silently accepted.

#### Scenario: Shell and Feed viewport matrix is complete

- **WHEN** responsive-layout verification finishes
- **THEN** reviewed screenshots exist at 1440px, 1200px, 1024px, and 800px for the standard Feed and Following Feed with the directory open, plus collapsed-directory and comments-open states

#### Scenario: Page screenshot set is complete

- **WHEN** redesign verification finishes
- **THEN** wide- and narrow-desktop screenshots exist for authentication, profile, messages, and upload and are reviewed for clipping, overflow, inconsistent density, and unreachable controls
