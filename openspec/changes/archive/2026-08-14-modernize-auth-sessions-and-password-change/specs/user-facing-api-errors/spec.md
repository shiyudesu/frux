## ADDED Requirements

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
