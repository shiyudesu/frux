# admin-authentication Specification

## Purpose

Define dedicated administrator credential issuance, purpose-bound authentication, and browser-session isolation from consumer login.

## Requirements

### Requirement: Dedicated Admin Login
Frux SHALL provide a dedicated admin login API and `/admin/login` Web route that authenticate existing account credentials without exposing consumer registration or sharing the consumer login session.

#### Scenario: Active privileged account logs in
- **WHEN** an account with valid credentials is active and its current registered role grants at least one admin permission
- **THEN** Frux returns an admin access credential and the current bounded admin principal

#### Scenario: Credentials or admin eligibility are invalid
- **WHEN** the account is unknown, the password is wrong, the account is inactive, or the current role grants no admin permission
- **THEN** Frux returns the same generic admin authentication failure without revealing which condition failed

#### Scenario: Visitor opens the admin login page
- **WHEN** a browser without an admin session navigates to `/admin/login`
- **THEN** the page presents only admin credential login and does not present registration

### Requirement: Purpose-Bound Admin Credential
Frux SHALL issue admin credentials with a distinct admin signing-key ring and key identifier, issuer, `admin_access` purpose, `frux-admin` audience, bounded expiration, account subject, token ID, issued-at time, not-before time, and account authentication version. Protected admin routes SHALL require that purpose and SHALL compare the credential authentication version with the authoritative current account principal.

#### Scenario: Admin token accesses an admin route
- **WHEN** a valid unexpired `admin_access` token with the admin audience, recognized admin key identifier, and current authentication version is supplied to a protected admin route
- **THEN** Frux authenticates its account subject and continues to current-account permission evaluation

#### Scenario: Consumer token accesses an admin route
- **WHEN** a cryptographically valid ordinary consumer access token is supplied to `/api/admin/*`
- **THEN** Frux returns the stable admin-authentication 401 response before the handler executes

#### Scenario: Admin token accesses a consumer route
- **WHEN** an `admin_access` token is supplied where a consumer access token is required
- **THEN** Frux rejects it as the wrong token purpose

#### Scenario: Admin credential expires
- **WHEN** the admin token expiration passes
- **THEN** subsequent admin requests return the stable admin-authentication 401 response

#### Scenario: Account password changes
- **WHEN** the account authentication version is incremented after the admin token was issued
- **THEN** the next protected admin request rejects the stale credential before the handler executes

#### Scenario: Admin signing key rotates
- **WHEN** a new admin key identifier becomes active while the previous key remains inside its bounded verification overlap
- **THEN** newly issued tokens use the new key and unexpired prior tokens remain verifiable only until their normal expiration or overlap deadline

#### Scenario: Legacy admin token reaches the compatibility deadline
- **WHEN** a shared-secret, missing-key-ID, or otherwise legacy admin token is presented after the explicit migration deadline
- **THEN** Frux rejects it even if its embedded expiration has not passed

### Requirement: Isolated Admin Web Session
The Web client SHALL keep admin credential, principal, permission, bootstrap, and logout state separate from the consumer session and SHALL validate persisted admin session data before use.

#### Scenario: User and admin sessions coexist
- **WHEN** the same browser tab has both a consumer session and a valid admin session
- **THEN** user APIs use only the consumer token and admin APIs use only the admin token

#### Scenario: Admin logs out
- **WHEN** an operator logs out from the admin shell
- **THEN** the Web client clears admin credentials, principal, permissions, and cached admin data without clearing the consumer session

#### Scenario: Consumer logs out
- **WHEN** a user logs out from the consumer application
- **THEN** the Web client clears only consumer session state and does not overwrite a valid admin session

#### Scenario: Stored admin data is malformed
- **WHEN** the Web client reads invalid or unrecognized admin session JSON
- **THEN** it discards the value and routes the visitor to the admin login state

### Requirement: Admin Session Bootstrap
The admin shell SHALL bootstrap only through the dedicated admin credential and the authoritative current-principal endpoint, clearing privileged cached data whenever authentication becomes invalid.

#### Scenario: Direct admin navigation has no session
- **WHEN** a visitor directly opens a protected `/admin/*` route without an admin credential
- **THEN** the Web client routes to `/admin/login` and preserves only a validated admin return destination

#### Scenario: Bootstrap token is no longer valid
- **WHEN** `/api/admin/me` returns the stable authentication 401 during bootstrap or refresh
- **THEN** the Web client clears the admin session and cached admin data and routes to `/admin/login`

#### Scenario: Current account no longer has the route permission
- **WHEN** `/api/admin/me` or a protected API returns the authoritative permission 403
- **THEN** the Web client renders a forbidden state and does not expose stale cached admin content
