## MODIFIED Requirements

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
