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

#### Scenario: Review queue refresh becomes forbidden
- **WHEN** a queue with cached rows receives an authoritative `403`
- **THEN** the Web client clears those rows and does not render the queue table in the forbidden state

#### Scenario: Review decision response is lost
- **WHEN** a submitted decision may have committed but its response is lost
- **THEN** retrying the same case and decision payload reuses the original idempotency key until success or a case/payload change

#### Scenario: Admin video page opens during the current minute
- **WHEN** the default creation window is initialized
- **THEN** its upper bound includes the full current minute rather than truncating to the minute start
