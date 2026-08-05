## MODIFIED Requirements

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
