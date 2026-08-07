## MODIFIED Requirements

### Requirement: Protected Media Delivery
Originals, private outputs, incomplete assets, and non-public creator previews SHALL remain owner-protected and SHALL NOT inherit public immutable caching. Owner asset access SHALL prefer a ready protected baseline or cover variant when available and SHALL otherwise fall back to the protected original.

#### Scenario: Non-owner requests a private output
- **WHEN** a user without current read permission requests a private or original asset
- **THEN** the asset is not disclosed

#### Scenario: Owner requests a protected output
- **WHEN** the immutable owner has current permission
- **THEN** Frux provides an authorized short-lived response without exposing reusable credentials or changing public eligibility

#### Scenario: Owner requests a ready video asset
- **WHEN** a protected browser baseline is ready for the owned asset
- **THEN** the access response resolves the baseline rather than the original source

#### Scenario: Owner requests a ready cover asset
- **WHEN** a protected ready cover variant exists
- **THEN** the access response resolves that cover variant

#### Scenario: Owner requests an incomplete asset
- **WHEN** no matching ready preview variant exists and the protected original remains available
- **THEN** Frux returns short-lived original access and the client handles possible browser incompatibility truthfully

#### Scenario: Protected preview response is cached
- **WHEN** a browser receives owner-protected preview access
- **THEN** the response and underlying object use private no-store behavior rather than public caching
