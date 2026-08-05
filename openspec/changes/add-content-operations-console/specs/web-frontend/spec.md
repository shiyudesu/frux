## ADDED Requirements

### Requirement: Typed Admin Route Surface
The hand-written Web router SHALL include typed admin review-list, review-detail, and video-operations routes, SHALL validate path identifiers, and SHALL load admin pages without introducing a routing library.

#### Scenario: Code navigates to review detail
- **WHEN** frontend code constructs an admin review-detail destination with a valid review ID
- **THEN** the route union and navigation helper accept it and normalize it to the canonical path

#### Scenario: Invalid admin identifier is supplied
- **WHEN** an admin URL contains a missing, non-numeric, or non-positive review identifier
- **THEN** route normalization returns a safe not-found destination without issuing an invalid API request

#### Scenario: Public user opens an admin URL
- **WHEN** a session without any admin permission navigates directly to an admin route
- **THEN** the Web client renders a forbidden or login state and does not expose cached admin data
