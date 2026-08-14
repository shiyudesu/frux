# web-frontend Specification

## Purpose

Defines the TypeScript architecture, type-safety guarantees, routing, session distribution, and behavior-preservation requirements for the web frontend.

## Requirements

### Requirement: TypeScript-only frontend source

All source files under `apps/web/src` SHALL be TypeScript (`.ts`/`.tsx`). No `.js`/`.jsx` source files SHALL remain, and `index.html` SHALL reference the TypeScript entry (`main.tsx`).

#### Scenario: No legacy JS source remains

- **WHEN** a developer lists files under `apps/web/src`
- **THEN** every file has a `.ts`, `.tsx`, or `.css` extension and `index.html` loads `/src/main.tsx`

### Requirement: Strict TypeScript configuration

`apps/web/tsconfig.json` SHALL enable `strict: true`, `noUnusedLocals`, `noUnusedParameters`, and `verbatimModuleSyntax`. Source code SHALL NOT contain `@ts-nocheck`, `@ts-expect-error`, or explicit `any` annotations.

#### Scenario: Strict flags are enabled

- **WHEN** a developer inspects `apps/web/tsconfig.json`
- **THEN** `strict`, `noUnusedLocals`, `noUnusedParameters`, and `verbatimModuleSyntax` are all `true`

#### Scenario: Suppression comments are absent

- **WHEN** a developer searches `apps/web/src` for `@ts-nocheck`, `@ts-expect-error`, or `: any`
- **THEN** no matches are found

### Requirement: Runtime-compatible React types

The installed `@types/react` and `@types/react-dom` packages SHALL use the same major version as the React and ReactDOM runtime packages.

#### Scenario: React type definitions match the runtime

- **WHEN** a developer inspects `apps/web/package.json`
- **THEN** React, ReactDOM, and their corresponding type-definition packages all use major version 18

### Requirement: Type-checked build gate

The `apps/web` build SHALL run `tsc --noEmit` before `vite build` (via the `build` script in `package.json`), so that any type error fails the build.

#### Scenario: Type error fails the build

- **WHEN** a type error is introduced in `apps/web/src` and `pnpm run build` is executed
- **THEN** the build fails with a TypeScript diagnostic and produces no `dist/` output

### Requirement: Modular source layout

`apps/web/src` SHALL be organized into `types.ts`, `api/` (typed request client plus per-domain modules), `session.tsx`, `router.tsx`, `pages/`, `components/`, and `hooks/`. The root `App.tsx` SHALL contain only provider composition and route dispatch.

#### Scenario: No monolithic component file

- **WHEN** a developer inspects `apps/web/src`
- **THEN** page components live in `pages/`, shared components in `components/`, data-fetching logic in `api/` and `hooks/`, and no single component file replicates the former 2810-line `App.jsx`

### Requirement: Typed API boundary

All HTTP requests to the backend SHALL go through a generic typed client (e.g. `apiRequest<T>`), and domain types (`Video`, `User`, `Comment`, paginated responses, and structured API errors) SHALL be declared in `types.ts` and used as request/response types. The typed client SHALL preserve HTTP status and stable API error code for control flow while treating legacy backend error text as diagnostic compatibility data rather than safe display content. Values read from `localStorage` SHALL be validated with type guards before use.

#### Scenario: API functions return typed results

- **WHEN** a developer calls a function in `api/` such as `fetchFeedPage`
- **THEN** its return type is a declared interface from `types.ts`, not an implicit or `any` type

#### Scenario: Stored user JSON is validated

- **WHEN** the app reads the stored user profile from `localStorage`
- **THEN** a type guard narrows the parsed JSON before it is used as a `User`

#### Scenario: Structured API error is received

- **WHEN** an API request returns a non-success JSON response
- **THEN** the typed client throws an API error that preserves the HTTP status and optional stable error code

#### Scenario: Legacy API error has no code

- **WHEN** a proxy, older server, or compatibility endpoint returns only the legacy `error` or `message` field
- **THEN** the typed client preserves the response for diagnostics but the user-visible resolver selects a safe status-based or caller-provided fallback

### Requirement: Context-based session distribution

Session state (token, current user, login/logout) and navigation SHALL be provided via React context/hooks (e.g. `useSession`, `useNavigate`) rather than multi-level prop drilling.

#### Scenario: Pages consume session via hook

- **WHEN** a developer inspects a page component such as `ProfilePage`
- **THEN** it obtains session and navigation from hooks, not from `session`/`onNavigate` props passed through intermediate layout components

### Requirement: Refresh-Backed Consumer Web Session
The Web client SHALL keep consumer access credentials only in memory, SHALL restore login state through the scoped HttpOnly refresh cookie, and SHALL centralize token refresh and authenticated-request retry in one typed session coordinator.

#### Scenario: User logs in
- **WHEN** consumer login succeeds
- **THEN** the Web client stores the access token only in memory, stores no bearer token in localStorage, activates protected asset access, and exposes the authenticated profile through `useSession`

#### Scenario: Authenticated page reloads
- **WHEN** the browser reloads with a valid refresh cookie but no in-memory access token
- **THEN** the session coordinator refreshes once, restores the profile and access token, and then renders the authenticated state

#### Scenario: Legacy local token exists
- **WHEN** startup finds the previous consumer access-token localStorage key
- **THEN** the Web client deletes it and does not treat it as proof of authentication

#### Scenario: Access token expires during an API request
- **WHEN** an authenticated request receives the stable invalid-access-token response and the refresh session remains valid
- **THEN** one shared refresh occurs and the original request is retried at most once with the replacement token

#### Scenario: Multiple requests expire together
- **WHEN** multiple authenticated requests encounter one expired access token concurrently
- **THEN** they share one in-flight refresh rather than independently rotating the same refresh credential

#### Scenario: Refresh proves the session invalid
- **WHEN** refresh reports an expired, revoked, replayed, or otherwise invalid session
- **THEN** the Web client clears access state, cached authenticated data, and the protected-asset active marker and transitions to login

#### Scenario: Password validation fails
- **WHEN** password change returns an incorrect-current-password or new-password validation error
- **THEN** the Web client keeps the existing authenticated session and presents the mapped inline error

#### Scenario: Password change succeeds
- **WHEN** password change returns a replacement access credential and refresh cookie
- **THEN** the Web client atomically adopts the new in-memory credential and keeps the initiating browser signed in

#### Scenario: User logs out in another tab
- **WHEN** one tab completes or locally initiates consumer logout
- **THEN** other tabs receive a browser-local logout signal and clear their in-memory consumer state without affecting the isolated admin session

### Requirement: Typed hand-rolled routing

The frontend SHALL keep its hand-rolled history-based router, with routes expressed as a TypeScript union type so that invalid navigation targets fail type checking. The router SHALL include a typed `/search` pathname and validated search-query parsing without adding a routing library.

#### Scenario: Invalid route is a compile error

- **WHEN** a developer navigates to a misspelled or nonexistent route string
- **THEN** `tsc --noEmit` reports a type error

#### Scenario: Search route is authored

- **WHEN** frontend code navigates to the search destination
- **THEN** `/search` is accepted by the route union and its encoded query parameters are parsed through typed helpers

#### Scenario: Invalid search tab is supplied

- **WHEN** the URL contains an unsupported search tab value
- **THEN** the route safely normalizes it to the default video category

### Requirement: Typed Admin Route Surface

The hand-written Web router SHALL include typed admin review-list, review-detail, and video-operations routes, SHALL validate path identifiers, and SHALL load admin pages without introducing a routing library.

#### Scenario: Code navigates to review detail

- **WHEN** frontend code constructs an admin review-detail destination with a valid review ID
- **THEN** the route union and navigation helper accept it and normalize it to the canonical path

#### Scenario: Invalid admin identifier is supplied

- **WHEN** an admin URL contains a missing, non-numeric, or non-positive review identifier
- **THEN** route normalization returns a safe not-found destination without issuing an invalid API request

#### Scenario: Public user opens an admin URL

- **WHEN** a session without any admin permission navigates directly to an admin route
- **THEN** the Web client renders a forbidden or login state and does not expose cached admin data

#### Scenario: Review queue refresh becomes forbidden

- **WHEN** a queue with cached rows receives an authoritative `403`
- **THEN** the Web client clears those rows and does not render the queue table in the forbidden state

#### Scenario: Review decision response is lost

- **WHEN** a submitted decision may have committed but its response is lost
- **THEN** retrying the same case and decision payload reuses the original idempotency key until success or a case/payload change

#### Scenario: Admin video page opens during the current minute

- **WHEN** the default creation window is initialized
- **THEN** its upper bound includes the full current minute rather than truncating to the minute start

### Requirement: Behavior-preserving refactor

The migration SHALL NOT change any user-visible behavior: routes, feed scenes, interactions (like/favorite/follow/comment), uploads, messages, and profile editing SHALL work exactly as before.

#### Scenario: Smoke test of all pages
- **WHEN** a developer manually exercises login, all four feed scenes, comments, messages, profile editing, upload, and work viewing after the migration
- **THEN** every flow behaves identically to the pre-migration frontend

### Requirement: Local Pre-Upload Video Preview
The Web upload page SHALL preview the selected local video and cover before creating upload sessions, and SHALL manage local object URLs without retaining files after replacement or unmount.

#### Scenario: User selects a video
- **WHEN** a supported local video file is selected
- **THEN** the upload page displays an in-page video player with controls and no network upload is required

#### Scenario: User also selects a cover
- **WHEN** both video and cover files are selected
- **THEN** the selected cover is used as the local video poster and remains independently visible in the preview metadata

#### Scenario: Selected file changes
- **WHEN** the user replaces the video or cover file
- **THEN** the prior object URL is revoked and the preview uses only the newly selected file

#### Scenario: Upload page unmounts
- **WHEN** the user leaves the upload page
- **THEN** all local video and cover object URLs created by the page are revoked

#### Scenario: Browser cannot preview the selected video
- **WHEN** the local file cannot be decoded by the browser
- **THEN** the page reports a local preview limitation without clearing the selection or starting an upload

### Requirement: Paired Upload Validation and Partial Retry
The Web upload page SHALL validate the complete selected video and cover pair before creating either upload session, SHALL report actionable per-file validation failures, and SHALL preserve completed uploads for unchanged files across retries.

#### Scenario: Required cover is initially missing
- **WHEN** a user submits valid metadata and a selected video without selecting a cover
- **THEN** the page asks for a cover and creates no video or cover upload session

#### Scenario: User corrects the missing cover
- **WHEN** the user selects a valid cover after the missing-cover message and submits again
- **THEN** the page uploads the selected pair without treating the prior local validation failure as an upload-session conflict

#### Scenario: Selected cover violates upload constraints
- **WHEN** the selected cover has an unsupported format or exceeds the cover size limit
- **THEN** the page identifies the cover constraint before creating either upload session

#### Scenario: One side completes before the other fails
- **WHEN** one selected media file completes upload and the paired upload fails
- **THEN** retrying with the unchanged completed file reuses its completed asset and uploads only the failed side

#### Scenario: User replaces only the failed cover
- **WHEN** a video upload is complete and the user replaces an invalid or failed cover
- **THEN** the page preserves the completed video result and creates a new upload identity only for the new cover

#### Scenario: Work creation fails transiently
- **WHEN** both media uploads completed but creating the video returns a transient failure
- **THEN** retrying reuses both completed assets and the same video-creation idempotency identity
