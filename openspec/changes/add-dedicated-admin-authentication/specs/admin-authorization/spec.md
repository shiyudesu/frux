## MODIFIED Requirements

### Requirement: Current Admin Principal Evaluation
Admin authorization SHALL first require a valid purpose-bound admin credential and SHALL then resolve the current account status and role for each privileged request rather than trusting any JWT role as the final authorization decision.

#### Scenario: Operator is demoted after token issuance
- **WHEN** a valid admin token names an admin role but the persisted account now has a non-privileged role
- **THEN** a subsequent admin request is denied before the token expires

#### Scenario: Operator account is disabled
- **WHEN** the persisted account is no longer active
- **THEN** every admin permission check fails even if the admin token remains cryptographically valid

#### Scenario: Token role differs from current role
- **WHEN** an admin token contains a stale or missing role snapshot
- **THEN** Frux derives permissions only from the current persisted account role

### Requirement: Declarative Admin Route Protection
Every `/api/admin` route except the dedicated login endpoint SHALL require a valid admin-purpose credential and declare its required permission through shared middleware before its handler executes.

#### Scenario: Principal lacks the required permission
- **WHEN** an admin-authenticated principal requests an admin route without its declared permission
- **THEN** Frux returns HTTP 403 with a stable forbidden code and the handler does not execute

#### Scenario: Admin authentication is missing
- **WHEN** a caller without a valid `admin_access` credential requests a protected admin route
- **THEN** Frux returns HTTP 401 with the stable admin-authentication error rather than a permission response

#### Scenario: Consumer authentication is supplied
- **WHEN** a caller supplies a valid consumer access token to a protected admin route
- **THEN** Frux returns the same stable admin-authentication 401 response and does not evaluate admin permissions
