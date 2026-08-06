## MODIFIED Requirements

### Requirement: Permission-Aware Admin Shell
The Web client SHALL provide an internal admin shell with a dedicated admin login and isolated admin session whose navigation and route affordances reflect the current server-confirmed admin permissions while treating backend authentication and authorization as authoritative.

#### Scenario: Reviewer opens the admin workspace
- **WHEN** the admin-authenticated principal has review permissions but not content-enforcement permission
- **THEN** review destinations are visible and content-enforcement controls are absent

#### Scenario: Visitor opens an admin route without admin authentication
- **WHEN** no valid admin session exists for a direct `/admin/*` navigation
- **THEN** the Web client routes to `/admin/login` rather than the consumer login or registration surface

#### Scenario: Consumer is already logged in
- **WHEN** a browser has a valid consumer session but no admin session and opens an admin route
- **THEN** the Web client still requires dedicated admin login and does not reuse the consumer token

#### Scenario: Stale client permission is rejected
- **WHEN** the client renders an action that the backend no longer authorizes
- **THEN** the Web client displays a stable forbidden state, clears affected cached admin data, and does not report success

#### Scenario: Admin authentication expires
- **WHEN** an admin API returns the authoritative admin-authentication 401
- **THEN** the shell clears only admin session data and returns to `/admin/login`
