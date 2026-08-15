# consumer-auth-sessions Specification

## Purpose

Defines secure consumer access credentials, durable refresh sessions, password changes, logout, and bounded legacy-token migration.

## Requirements

### Requirement: Strict Short-Lived Consumer Access Credential
Frux SHALL issue consumer access JWTs with a separate consumer signing key, required key identifier, issuer, consumer audience, access purpose, account subject, refresh-session identifier, authentication version, token ID, issued-at time, not-before time, and expiration. Consumer access lifetime SHALL default to five minutes and SHALL NOT be configurable above fifteen minutes.

#### Scenario: Valid consumer access token is supplied
- **WHEN** a request supplies a correctly signed, unexpired consumer token whose required claims and key identifier are valid
- **THEN** Frux authenticates the account and exposes only the account and session identity required by the consumer request

#### Scenario: Consumer token has stale role data
- **WHEN** consumer access is issued
- **THEN** the token does not contain an authorization role that can become a stale source of permission truth

#### Scenario: Token purpose or audience is wrong
- **WHEN** an admin token or another cryptographically valid token is supplied to a consumer-protected route
- **THEN** Frux returns the stable invalid-access-token response before the handler executes

#### Scenario: Consumer TTL exceeds the maximum
- **WHEN** configuration requests a consumer access lifetime longer than fifteen minutes
- **THEN** Frux fails startup instead of issuing overlong credentials

### Requirement: Durable Refresh Session
Frux SHALL establish consumer refresh sessions in PostgreSQL and SHALL store only bounded session metadata and a cryptographic hash of each opaque refresh secret.

#### Scenario: Password login succeeds
- **WHEN** an active account supplies valid consumer credentials
- **THEN** Frux creates a refresh session, sets its opaque credential in a scoped HttpOnly cookie, and returns a short-lived consumer access token

#### Scenario: Database contents are inspected
- **WHEN** an operator reads a refresh-session row
- **THEN** the row contains no raw refresh token, password, access token, or reusable browser credential

#### Scenario: Refresh session expires
- **WHEN** the fixed refresh-session expiration has passed
- **THEN** refresh is rejected, no replacement credential is issued, and the browser must authenticate again

#### Scenario: Account is no longer active
- **WHEN** a refresh session belongs to an inactive or unavailable account
- **THEN** Frux rejects refresh without issuing a new access or refresh credential

### Requirement: Rotating Refresh Credential
Every successful refresh SHALL atomically validate and rotate the current refresh secret, SHALL bind the new access token to the same durable session, and SHALL detect reuse without misclassifying an ordinary concurrent-tab race.

#### Scenario: Current refresh token is used
- **WHEN** an unexpired active refresh credential matches the session's current secret hash
- **THEN** Frux replaces the secret hash, returns a new refresh cookie, and issues a new short-lived access token

#### Scenario: Immediately previous token races
- **WHEN** a concurrent request presents the immediately previous refresh secret inside the bounded grace interval
- **THEN** Frux returns a stable superseded-refresh conflict without revoking the token family or clearing the newer cookie

#### Scenario: Superseded token is replayed after grace
- **WHEN** a previously rotated refresh secret is presented after the grace interval
- **THEN** Frux revokes the token family and requires fresh credential login

#### Scenario: Two refreshes target the same current secret
- **WHEN** two requests concurrently attempt to rotate one current refresh credential
- **THEN** at most one request performs the current-secret rotation and no response restores an older cookie

### Requirement: Durable Consumer Logout
Consumer logout SHALL revoke the current durable refresh session when its cookie is valid and SHALL remain safe and idempotent when the access token is missing, expired, or already cleared.

#### Scenario: Logged-in browser logs out
- **WHEN** the browser deletes the current consumer session
- **THEN** Frux revokes the refresh session, expires the refresh cookie, and the Web client immediately clears its in-memory access state and protected-asset active marker

#### Scenario: Logout request is repeated
- **WHEN** logout is retried after the session was already revoked or the cookie was removed
- **THEN** Frux returns the same successful logout outcome without recreating or refreshing credentials

#### Scenario: Old access token is used after logout
- **WHEN** an already issued consumer access token is used before its short expiration
- **THEN** it can remain cryptographically valid only until its bounded access-token expiration and cannot be refreshed

### Requirement: Authenticated Password Change
An authenticated consumer SHALL be able to replace the account password by supplying the correct current password and a compliant different new password. Password replacement, authentication-version increment, revocation of prior refresh sessions, and creation of the initiating browser's replacement session SHALL be one durable account transaction.

#### Scenario: Current password and new password are valid
- **WHEN** an authenticated active account supplies its correct current password and a compliant different new password
- **THEN** Frux updates the password hash, increments the authentication version, revokes all prior refresh sessions, creates a replacement session for the initiating browser, and returns a new access credential

#### Scenario: Current password is wrong
- **WHEN** an authenticated user supplies an incorrect current password
- **THEN** Frux returns the stable current-password error without changing the password, revoking sessions, or treating the existing login state as expired

#### Scenario: New password matches the current password
- **WHEN** the proposed new password authenticates against the current password hash
- **THEN** Frux rejects it with the stable unchanged-password validation error

#### Scenario: Concurrent password changes use the same old credential
- **WHEN** two password-change requests both verify the same old password concurrently
- **THEN** at most one replacement commits and the other receives a stable credential-changed conflict

#### Scenario: Password persistence fails
- **WHEN** the durable password-change transaction fails
- **THEN** the old password, authentication version, and refresh sessions remain unchanged

### Requirement: Shared Password Policy
Registration and password change SHALL use one domain password policy that preserves case and internal whitespace, requires at least eight Unicode code points, and rejects values longer than 72 UTF-8 bytes before bcrypt hashing.

#### Scenario: New registration uses a short password
- **WHEN** a new account submits fewer than eight Unicode code points as its password
- **THEN** registration returns the stable account password-validation error and creates no account

#### Scenario: Password exceeds the bcrypt boundary
- **WHEN** registration or password change submits more than 72 UTF-8 bytes
- **THEN** Frux returns a validation error rather than an internal hashing failure

#### Scenario: Existing account has a legacy short password
- **WHEN** an existing account supplies its correct previously accepted short password at login
- **THEN** authentication remains possible, but any newly selected password must satisfy the current policy

#### Scenario: Password contains uppercase or internal spaces
- **WHEN** a compliant password contains uppercase letters or internal whitespace
- **THEN** Frux preserves those characters for case-sensitive authentication

### Requirement: Consumer Login Hardening
Consumer password login SHALL authenticate only normal active accounts. It SHALL keep unknown accounts, wrong passwords, cancelled accounts, and unsupported inactive states indistinguishable through `AUTH_INVALID_CREDENTIALS` and SHALL perform equivalent bounded password-hash work for unknown-account and existing-account failure paths. A frozen status SHALL be disclosed as HTTP 423 with `AUTH_ACCOUNT_FROZEN` only after the submitted password authenticates against the account's current credential.

#### Scenario: Unknown account attempts login
- **WHEN** a syntactically valid unknown account submits a password
- **THEN** Frux performs dummy bcrypt comparison and returns the same status, code, body category, and user message as a wrong password

#### Scenario: Existing account supplies a wrong password
- **WHEN** an existing account supplies an incorrect password
- **THEN** Frux returns the generic invalid-credentials result without revealing account existence

#### Scenario: Frozen account supplies the correct password
- **WHEN** a frozen account submits its current correct password
- **THEN** Frux returns HTTP 423 with `AUTH_ACCOUNT_FROZEN` and creates no authentication credential or refresh session

#### Scenario: Cancelled or unsupported inactive account supplies the correct password
- **WHEN** a cancelled account or an account in another unsupported inactive state submits its current correct password
- **THEN** Frux returns `AUTH_INVALID_CREDENTIALS` and creates no authentication credential or refresh session

### Requirement: Protected Asset Credential Synchronization
The protected-media asset credential SHALL use the current short-lived consumer access credential and SHALL rotate on login, refresh, and password change without becoming a longer-lived authentication path.

#### Scenario: Access token is refreshed
- **WHEN** the browser successfully refreshes its consumer session
- **THEN** Frux replaces the scoped HttpOnly asset cookie with the new short-lived access token

#### Scenario: Browser logs out while offline
- **WHEN** the Web client cannot reach the logout endpoint
- **THEN** it still clears the protected-asset active marker so a stale asset cookie is not used by that browser session

#### Scenario: Asset credential expires
- **WHEN** the short-lived asset JWT passes its expiration
- **THEN** protected asset authorization rejects it even if the cookie remains stored

### Requirement: Bounded Legacy JWT Migration
Frux SHALL support already-issued legacy access tokens only through an explicit deployment deadline and SHALL reject no-audience, no-key-ID, or shared-secret legacy tokens after that deadline.

#### Scenario: Legacy token is used during overlap
- **WHEN** an unexpired legacy token is presented before the configured compatibility deadline
- **THEN** Frux validates it through the bounded legacy path without issuing another legacy token

#### Scenario: Compatibility deadline passes
- **WHEN** the explicit compatibility deadline has elapsed
- **THEN** Frux accepts only strict keyed credentials and rejects the legacy token

#### Scenario: Compatibility deadline is unsafe
- **WHEN** configuration would remove legacy verification before the maximum previously issued token lifetime has elapsed
- **THEN** startup validation rejects the unsafe migration configuration

### Requirement: Privileged Ordinary Account Session Revocation
Privileged freeze, unfreeze, and force-sign-out operations SHALL increment the ordinary account authentication version. Freeze and force-sign-out SHALL revoke every active durable refresh session in the same transaction; unfreeze SHALL NOT restore or replace any previously revoked session.

#### Scenario: Frozen account attempts refresh
- **WHEN** a refresh credential belongs to an account frozen by a committed privileged operation
- **THEN** refresh is rejected and no replacement access or refresh credential is issued

#### Scenario: Forced-out account attempts refresh
- **WHEN** a refresh credential predates a committed force-sign-out operation
- **THEN** its revoked state or stale authentication version prevents refresh

#### Scenario: Account is unfrozen
- **WHEN** a frozen account returns to normal status
- **THEN** the user must complete a new password login because all prior durable sessions remain revoked

#### Scenario: Previously issued access token is used
- **WHEN** a consumer access token was issued before freeze or force-sign-out and has not reached its short expiration
- **THEN** it may remain cryptographically valid only until that existing expiration, can read already-delivered account messages through the normal message API, and cannot be refreshed afterward

### Requirement: Password-Proven Frozen Login Result
Consumer password login SHALL return a dedicated frozen-account result only after the submitted password has authenticated against the current account credential. The result SHALL issue no access token, refresh credential, refresh-session row, asset cookie, or replacement session.

#### Scenario: Frozen account submits the correct password
- **WHEN** a frozen ordinary account submits its current correct password
- **THEN** Frux returns HTTP 423 with `AUTH_ACCOUNT_FROZEN` and creates no authentication credential

#### Scenario: Frozen account submits a wrong password
- **WHEN** a frozen account submits an incorrect password
- **THEN** Frux returns the same `AUTH_INVALID_CREDENTIALS` response as any other wrong password and does not reveal the frozen state

#### Scenario: Unknown account attempts login
- **WHEN** a syntactically valid unknown account submits a password
- **THEN** Frux performs the existing dummy bcrypt work and returns `AUTH_INVALID_CREDENTIALS`

#### Scenario: Cancelled account submits the correct password
- **WHEN** a cancelled account submits its previously valid password
- **THEN** Frux retains the generic invalid-credentials result and creates no session

#### Scenario: Frozen account is later unfrozen
- **WHEN** the account returns to normal status
- **THEN** a subsequent correct password login can create a new session while all credentials revoked by the earlier freeze remain unusable
