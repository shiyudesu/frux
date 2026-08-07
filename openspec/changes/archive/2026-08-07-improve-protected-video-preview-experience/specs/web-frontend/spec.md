## ADDED Requirements

### Requirement: Local Pre-Upload Video Preview
The Web upload page SHALL preview the selected local video and cover before creating upload sessions, and SHALL manage local object URLs without retaining files after replacement or unmount.

#### Scenario: User selects a video
- **WHEN** a supported local video file is selected
- **THEN** the upload page displays an in-page video player with controls and no network upload is required

#### Scenario: User also selects a cover
- **WHEN** both video and cover files are selected
- **THEN** the selected cover is used as the local video poster and remains independently visible in the preview metadata

#### Scenario: Selected file changes
- **WHEN** the user replaces the video or cover file
- **THEN** the prior object URL is revoked and the preview uses only the newly selected file

#### Scenario: Upload page unmounts
- **WHEN** the user leaves the upload page
- **THEN** all local video and cover object URLs created by the page are revoked

#### Scenario: Browser cannot preview the selected video
- **WHEN** the local file cannot be decoded by the browser
- **THEN** the page reports a local preview limitation without clearing the selection or starting an upload
