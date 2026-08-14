# user-facing-api-errors Specification

## Purpose

Defines stable, secure API error contracts and safe, consistent user-visible error handling across Frux.

## Requirements

### Requirement: Stable API error envelope
Every non-success JSON response produced by Frux API and upload handlers SHALL include a stable machine-readable `code` alongside the existing `error` field. Error codes SHALL be independent of human language and internal implementation text, while HTTP statuses SHALL continue to represent the existing validation, authentication, authorization, not-found, conflict, throttling, availability, and internal-error categories.

#### Scenario: Known business error is returned
- **WHEN** an HTTP handler maps a known domain or application error
- **THEN** the response contains the existing HTTP status, a stable error `code`, and the backward-compatible `error` field

#### Scenario: Unexpected internal failure is returned
- **WHEN** an HTTP handler encounters an unexpected repository, cache, queue, or internal service failure
- **THEN** the response uses an internal or unavailable error code and does not expose stack traces, database details, credentials, object keys, or wrapped infrastructure error text

#### Scenario: Existing API client reads the response
- **WHEN** a client ignores the new `code` field and continues reading the existing `error` field
- **THEN** the response remains compatible with the previous error contract

### Requirement: Indistinguishable credential failure
Password login SHALL use the same HTTP status, error code, legacy error text, and user-visible Web message when the submitted account does not exist or the submitted password is incorrect.

#### Scenario: Account does not exist
- **WHEN** a user submits a syntactically valid account that is not registered
- **THEN** the API returns `401` with the invalid-credentials code and the Web displays `账号或密码错误，请重新输入`

#### Scenario: Password is incorrect
- **WHEN** a user submits an incorrect password for an existing account
- **THEN** the API returns the same response category as an unknown account and the Web displays `账号或密码错误，请重新输入`

### Requirement: Safe frontend error resolution
The Web client SHALL convert API error codes, HTTP statuses, network failures, and explicitly marked local validation errors into user-understandable Chinese messages through one shared resolver. It MUST NOT render raw backend, framework, browser, or arbitrary JavaScript error text as user-visible content.

#### Scenario: Known API error code is received
- **WHEN** an API response contains a recognized error code
- **THEN** the shared resolver returns the configured Chinese message for that code

#### Scenario: Network request fails
- **WHEN** `fetch` or an upload transport fails before receiving an HTTP response
- **THEN** the Web displays `网络连接失败，请检查网络后重试` instead of browser text such as `Failed to fetch`

#### Scenario: Unknown server error is received
- **WHEN** an API response has status `500` or greater and its code is unknown or absent
- **THEN** the Web displays an action-appropriate temporary-failure message and does not display the response's raw error text

#### Scenario: Unknown client error is received
- **WHEN** an API response has an unrecognized `4xx` code
- **THEN** the Web displays the caller's action-appropriate fallback and does not display the response's raw error text

#### Scenario: Local validation fails
- **WHEN** frontend validation intentionally creates an explicitly user-facing local error
- **THEN** the resolver may display that authored message without treating arbitrary `Error.message` values as safe

### Requirement: Consistent user-visible error surfaces
Authentication, feed loading, comments, relations, messages, profile editing, public profiles, creator content, personal libraries, recommendation feedback, and uploads SHALL use the shared error resolver for visible API or transport failures.

#### Scenario: User-visible operation fails
- **WHEN** any covered page, hook, or component presents an API or transport failure
- **THEN** the displayed text comes from the shared resolver rather than directly from `ApiError.message`, `Error.message`, response `error`, or response `message`

#### Scenario: Existing actionable search error occurs
- **WHEN** search validation or availability fails
- **THEN** the existing actionable Chinese search messages remain available through the shared error-code and fallback policy

#### Scenario: Authentication token is no longer valid
- **WHEN** an authenticated flow receives the invalid-access-token code
- **THEN** the existing session-clear and login-navigation behavior is preserved instead of replacing it with a generic inline error

### Requirement: Password-Change Error Semantics
Password-change failures SHALL use stable codes that distinguish current-password mistakes, new-password validation, unchanged credentials, concurrent credential replacement, throttling, and temporary service failure without exposing password hashes or internal errors.

#### Scenario: Current password is incorrect
- **WHEN** an authenticated user submits the wrong current password
- **THEN** the API returns the stable current-password error and the Web displays an actionable inline message without clearing the session

#### Scenario: New password is invalid
- **WHEN** the new password violates length or bcrypt-byte limits
- **THEN** the API returns the stable password-validation code and the Web displays the authored password rule

#### Scenario: Password was changed concurrently
- **WHEN** compare-and-swap persistence detects that another request already replaced the credential
- **THEN** the API returns a stable conflict code and the Web asks the user to authenticate again rather than reporting success

### Requirement: Refresh-Session Error Semantics
Refresh-session failures SHALL distinguish invalid or replayed sessions that require login from a concurrent superseded refresh that can be retried, and SHALL never expose raw cookie, token, hash, or persistence data.

#### Scenario: Refresh session is invalid
- **WHEN** the refresh credential is expired, revoked, malformed, mismatched, or belongs to an inactive account
- **THEN** the API returns the stable invalid-refresh-session response and the Web clears consumer authentication state

#### Scenario: Refresh token replay is detected
- **WHEN** a rotated secret is reused outside the concurrency grace interval
- **THEN** the API returns the stable replay response, revokes the token family, and the Web requires credential login

#### Scenario: Concurrent refresh was superseded
- **WHEN** the immediately previous refresh secret is observed inside the allowed race interval
- **THEN** the API returns a stable conflict that does not clear the newer cookie and the Web retries through its shared coordinator

#### Scenario: Refresh infrastructure is unavailable
- **WHEN** session persistence or required authentication infrastructure cannot safely decide refresh
- **THEN** the API returns a stable temporary-unavailability code and does not issue a success-shaped fallback credential
