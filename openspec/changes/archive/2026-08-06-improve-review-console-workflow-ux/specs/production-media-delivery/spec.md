## ADDED Requirements

### Requirement: Reviewer-Protected Media Delivery
Frux SHALL provide authorized review readers with short-lived access to the protected media and cover for the current review subject without changing public media eligibility or disclosing reusable storage credentials.

#### Scenario: Reviewer requests a pending subject preview
- **WHEN** an active principal with `review.read` requests preview access for a current non-deleted review subject
- **THEN** Frux returns typed media access that expires within five minutes and leaves the video's public media projection unchanged

#### Scenario: Public caller reuses the review preview
- **WHEN** a caller without current review authorization attempts to obtain protected preview access
- **THEN** Frux denies the request without revealing the protected object location

#### Scenario: Review subject version is stale
- **WHEN** the requested case no longer matches the video's current review version or reviewable lifecycle
- **THEN** Frux returns a stable conflict or unavailable response and does not issue preview access

#### Scenario: Preview access expires
- **WHEN** the signed review preview lifetime has elapsed
- **THEN** storage or local media delivery rejects the URL and a still-authorized reviewer must request fresh access
