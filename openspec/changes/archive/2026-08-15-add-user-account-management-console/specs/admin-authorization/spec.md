## ADDED Requirements

### Requirement: Ordinary Account Management Permission
Frux SHALL register the `account.manage` permission for privileged ordinary-user account discovery and enforcement. The compatibility `admin` role SHALL receive this permission, while `reviewer`, `operator`, `user`, and unknown roles SHALL NOT receive it.

#### Scenario: Admin manages an ordinary account
- **WHEN** an active current principal with the `admin` role requests an account-management route
- **THEN** the shared permission middleware authorizes the request with `account.manage`

#### Scenario: Operator requests account management
- **WHEN** an active operator requests an account-management route
- **THEN** Frux returns the stable admin-permission-denied response and the account handler does not execute

#### Scenario: Consumer token requests account management
- **WHEN** a caller supplies a valid consumer access token to an account-management route
- **THEN** Frux returns the stable admin-authentication 401 response before evaluating `account.manage`

