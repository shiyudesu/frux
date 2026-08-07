## ADDED Requirements

### Requirement: Owner-Protected Work Preview
An authenticated creator SHALL be able to open every owned non-deleted work from the creator management surface, including pending-review, processing, rejected, private, and offline states, using owner-authorized temporary media access without changing public eligibility.

#### Scenario: Creator opens a pending work
- **WHEN** the owner activates a pending-review work whose public media URL is blank
- **THEN** the Web requests short-lived access for its media and cover assets and opens the protected WorkViewer

#### Scenario: Creator opens a processed non-public work
- **WHEN** a ready baseline exists for an owned private, pending, rejected, or offline video
- **THEN** the viewer plays the protected baseline and does not expose it through public detail, Feed, or anonymous media routes

#### Scenario: Work is still processing
- **WHEN** only the protected uploaded source or cover is available
- **THEN** the viewer shows the available owner preview with truthful processing state and does not claim that the video is published

#### Scenario: Browser cannot decode the protected source
- **WHEN** the current protected source uses an unsupported browser codec
- **THEN** the viewer retains the cover and metadata, reports that preview playback is unavailable while processing, and offers retry

#### Scenario: Another user requests preview
- **WHEN** a non-owner requests protected asset access for the work
- **THEN** Frux denies access without returning a reusable media location

#### Scenario: Creator closes the viewer
- **WHEN** the owner closes WorkViewer or selects another work
- **THEN** stale protected-access responses cannot reopen or overwrite the current viewer
