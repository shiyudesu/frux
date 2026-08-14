## MODIFIED Requirements

### Requirement: Aggregated Profile Summary
Frux SHALL return an authenticated profile summary containing the owner's display profile, account identifier, gender, following count, follower count, public work count, received-like count, and profile privacy settings. Public profile responses SHALL omit the account identifier and contain only fields allowed for public display.

#### Scenario: Current user opens profile
- **WHEN** an authenticated user requests their profile
- **THEN** the response includes their own account identifier, display fields, relationship counts, public work count, received-like count, gender, and profile settings

#### Scenario: Visitor opens public profile
- **WHEN** a visitor requests another user's public profile
- **THEN** the response omits the account identifier and other private account data and returns only public profile statistics and settings-derived capabilities

#### Scenario: Visitor profile renders identity
- **WHEN** the Web renders another user's public profile
- **THEN** it displays nickname-based public identity without an account row or account fallback

## ADDED Requirements

### Requirement: Public Profile Cache Privacy
The Web SHALL NOT persist another user's account identifier in the public-profile cache and SHALL sanitize legacy cache entries before using them.

#### Scenario: Public profile is cached
- **WHEN** the Web caches profile data used to navigate to or render another user's profile
- **THEN** the stored entry contains the user ID and public presentation fields without an account identifier

#### Scenario: Legacy account-bearing cache is read
- **WHEN** the Web reads a public-profile cache created before account identifiers became private
- **THEN** it ignores and removes the cached account field before the entry can be rendered or retained
