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
