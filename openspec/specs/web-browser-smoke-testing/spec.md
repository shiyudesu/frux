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

The browser smoke workflow SHALL verify the redesigned application shell at wide and narrow desktop widths using stable `data-ui` markers and element geometry.

#### Scenario: Wide desktop shell geometry

- **WHEN** the smoke workflow opens a user route at 1440px viewport width
- **THEN** the side navigation, top header, main content, and active route marker are visible, the side navigation is 160px wide, the header is 56px high, and the document has no horizontal overflow

#### Scenario: Narrow desktop shell geometry

- **WHEN** the smoke workflow opens a user route below 901px viewport width
- **THEN** the compact desktop side navigation and top header remain visible and no mobile bottom navigation is rendered

### Requirement: Redesigned Feed panel coverage

The browser smoke workflow SHALL verify the Feed stage and threaded comment surface before and after opening comments at wide and narrow desktop widths.

#### Scenario: Desktop comments reduce player width

- **WHEN** the smoke workflow records the Feed-stage width, opens comments at 1440px, and records the width again
- **THEN** the 346px details panel is visible, the player width is smaller than its closed state, and the action rail remains inside the player column

#### Scenario: Desktop thread interaction remains visible

- **WHEN** the workflow expands replies, selects a reply target, and toggles a comment like on desktop
- **THEN** the thread controls, selected target, composer, and updated like state remain visible without clipping or horizontal overflow

#### Scenario: Narrow desktop comments use side drawer

- **WHEN** the smoke workflow opens comments below 901px viewport width
- **THEN** the details panel is presented as a right-side drawer and sorting, expansion, reply composition, and closing remain operable without changing the active Feed item

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

### Requirement: Redesign visual evidence

The browser verification run SHALL capture wide- and narrow-desktop screenshots for the shell, Feed with comments closed, Feed with comments open, authentication, profile, messages, and upload. Remaining visual differences SHALL be documented rather than silently accepted.

#### Scenario: Screenshot set is complete

- **WHEN** redesign verification finishes
- **THEN** the required wide- and narrow-desktop screenshots exist for all specified routes and states and are reviewed for layout, clipping, overflow, and inconsistent tokens
