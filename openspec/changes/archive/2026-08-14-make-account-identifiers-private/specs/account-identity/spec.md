## ADDED Requirements

### Requirement: Private Account Identifier Boundary
Frux SHALL treat the canonical account identifier as private credential data. The system MAY accept it at credential and privileged administration boundaries and SHALL return it to the authenticated account owner, but user-facing responses about another user MUST omit it.

#### Scenario: Owner reads authenticated profile
- **WHEN** an authenticated user requests their own profile
- **THEN** the response includes that user's canonical account identifier

#### Scenario: User-facing response describes another user
- **WHEN** a profile, search, relationship, comment, or other user-facing response describes a user other than the authenticated owner
- **THEN** the response contains no account identifier for that user

#### Scenario: Visitor attempts account-based discovery
- **WHEN** a visitor submits a user-discovery query that matches only another user's account identifier
- **THEN** Frux returns no match based on that identifier

#### Scenario: Privileged account operation reads identity
- **WHEN** an authorized administration or trusted internal account operation requires the canonical account identifier
- **THEN** the identifier remains available within that privileged boundary
