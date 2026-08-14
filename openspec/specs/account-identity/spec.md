# account-identity Specification

## Purpose
Define canonical account identity behavior and the shared normalization boundary used throughout Frux.

## Requirements

### Requirement: Canonical Account Identity
Frux SHALL canonicalize account identifiers by trimming surrounding whitespace and converting letters to lowercase before registration, persistence, uniqueness checks, and login lookup.

#### Scenario: User registers with mixed-case account
- **WHEN** a user registers with the account value `Alice`
- **THEN** the account is persisted and returned as `alice`

#### Scenario: User logs in with a case variant
- **WHEN** the persisted account is `alice` and the user attempts login with `ALICE`
- **THEN** the login lookup resolves the same account before password authentication

#### Scenario: User registers a duplicate case variant
- **WHEN** account `alice` already exists and another registration uses `Alice`
- **THEN** registration returns the existing account-conflict behavior

#### Scenario: Account contains surrounding whitespace
- **WHEN** registration or login receives an account with surrounding whitespace
- **THEN** the whitespace is removed before validation and lookup

### Requirement: Account Normalization Boundary
Account normalization SHALL be implemented once as shared domain behavior and SHALL be used consistently by account creation, restoration, and repository lookup inputs.

#### Scenario: Account is restored from persistence
- **WHEN** an account entity is reconstructed from a stored record
- **THEN** its exposed account identifier uses the same canonical lowercase representation

#### Scenario: Nickname and password are processed
- **WHEN** account normalization occurs
- **THEN** it does not lowercase the nickname or password value

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
