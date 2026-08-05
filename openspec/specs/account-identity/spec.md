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
