## ADDED Requirements

### Requirement: Closed Admin Permission Registry
Frux SHALL evaluate privileged access through a closed registry of named admin permissions and SHALL deny unknown roles, permissions, or mappings.

#### Scenario: Reviewer reads review work
- **WHEN** an authenticated active principal with `review.read` requests an endpoint requiring `review.read`
- **THEN** the request is authorized without granting unrelated content, configuration, governance, or audit permissions

#### Scenario: Unknown role reaches an admin endpoint
- **WHEN** an authenticated principal has a role that is not registered for admin authorization
- **THEN** the request is denied and no admin permission is inferred from the role string

### Requirement: Current Admin Principal Evaluation
Admin authorization SHALL resolve the current account status and role for each privileged request rather than trusting the JWT role as the final authorization decision.

#### Scenario: Operator is demoted after token issuance
- **WHEN** a valid access token names an admin role but the persisted account now has a non-privileged role
- **THEN** a subsequent admin request is denied before the token expires

#### Scenario: Operator account is disabled
- **WHEN** the persisted account is no longer active
- **THEN** every admin permission check fails even if the access token remains cryptographically valid

### Requirement: Declarative Admin Route Protection
Every `/api/admin` route SHALL declare its required permission through shared middleware before its handler executes.

#### Scenario: Principal lacks the required permission
- **WHEN** an authenticated principal requests an admin route without its declared permission
- **THEN** Frux returns HTTP 403 with a stable forbidden code and the handler does not execute

#### Scenario: Authentication is missing
- **WHEN** a caller without a valid access token requests an admin route
- **THEN** Frux returns the existing HTTP 401 access-token error rather than a permission response

### Requirement: Compatible Bootstrap Admin
Existing accounts with the canonical `admin` role SHALL receive the complete initial registered permission set until a later operator-management change replaces that bootstrap behavior.

#### Scenario: Existing admin accesses a protected route
- **WHEN** an existing active `admin` account requests any route protected by an initial registered permission
- **THEN** the request is authorized without requiring a data migration to individual permissions
