## ADDED Requirements

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
