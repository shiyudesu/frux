## ADDED Requirements

### Requirement: Authentication Session Policies
Consumer login, refresh, and password-change endpoints SHALL use registered rate-limit policies appropriate to their authenticated or unauthenticated identity boundary and SHALL retain bounded enforcement when distributed coordination is unavailable.

#### Scenario: Consumer login is attempted repeatedly
- **WHEN** one trusted proxy-normalized client IP repeatedly calls the consumer login endpoint
- **THEN** the registered consumer-login policy limits the requests without accepting an account string as the quota identity

#### Scenario: Refresh is attempted repeatedly
- **WHEN** one trusted proxy-normalized client IP repeatedly calls the cookie refresh endpoint
- **THEN** the registered refresh policy applies a bounded quota before refresh-session processing

#### Scenario: Password change is attempted repeatedly
- **WHEN** an authenticated account repeatedly submits password-change attempts
- **THEN** the registered password-change policy uses the server-derived user ID and rejects excess attempts before bcrypt verification

#### Scenario: Redis is unavailable during login or refresh
- **WHEN** distributed coordination fails for consumer login or refresh
- **THEN** the declared stricter local fallback decides the request rather than making authentication unlimited

#### Scenario: Redis is unavailable during password change
- **WHEN** distributed coordination fails for the password-change fail-closed policy
- **THEN** Frux returns the stable rate-limit availability response and does not attempt to change credentials
