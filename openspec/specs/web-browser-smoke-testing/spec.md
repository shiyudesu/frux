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

The smoke test SHALL visit timeline, recommendation, following, and hot feed scenes and SHALL exercise available like, favorite, follow, navigation, swipe, and comment interactions without runtime failures.

#### Scenario: All feed scenes render

- **WHEN** the test visits each of the four feed routes
- **THEN** every route renders its expected feed state without an application crash

#### Scenario: Feed interaction persists

- **WHEN** the test performs an available like, favorite, follow, or comment action
- **THEN** the interface reflects the successful action and the corresponding API request succeeds

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
