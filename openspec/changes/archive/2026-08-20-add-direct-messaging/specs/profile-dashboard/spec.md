## ADDED Requirements

### Requirement: Public profiles expose truthful private-message eligibility
An authenticated visitor viewing another user's public profile SHALL receive an independently loaded private-message eligibility state. The profile SHALL expose a private-message action only with truthful mutual-follow guidance and SHALL open the canonical conversation when messaging is eligible.

#### Scenario: Mutually following visitor opens a public profile
- **WHEN** the authenticated visitor and profile owner currently follow one another
- **THEN** the profile presents an enabled private-message action that resolves and opens their canonical conversation

#### Scenario: Visitor is not mutually following
- **WHEN** either follow direction is inactive
- **THEN** the profile does not present an enabled send action and explains that mutual follow is required

#### Scenario: User views their own public profile
- **WHEN** the authenticated user opens their own public-profile route
- **THEN** no self-message action is rendered and the existing own-profile navigation remains available

#### Scenario: Routed profile changes during eligibility loading
- **WHEN** an eligibility response for one user arrives after the routed public-profile user ID changes
- **THEN** the stale response cannot enable, disable, or navigate the new profile's private-message action

#### Scenario: Eligibility changes before conversation creation
- **WHEN** the profile showed eligibility but mutual follow is removed before the visitor activates the action
- **THEN** the authoritative create-conversation request fails safely and the profile refreshes to an ineligible state

