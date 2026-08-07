## ADDED Requirements

### Requirement: Paired Upload Validation and Partial Retry
The Web upload page SHALL validate the complete selected video and cover pair before creating either upload session, SHALL report actionable per-file validation failures, and SHALL preserve completed uploads for unchanged files across retries.

#### Scenario: Required cover is initially missing
- **WHEN** a user submits valid metadata and a selected video without selecting a cover
- **THEN** the page asks for a cover and creates no video or cover upload session

#### Scenario: User corrects the missing cover
- **WHEN** the user selects a valid cover after the missing-cover message and submits again
- **THEN** the page uploads the selected pair without treating the prior local validation failure as an upload-session conflict

#### Scenario: Selected cover violates upload constraints
- **WHEN** the selected cover has an unsupported format or exceeds the cover size limit
- **THEN** the page identifies the cover constraint before creating either upload session

#### Scenario: One side completes before the other fails
- **WHEN** one selected media file completes upload and the paired upload fails
- **THEN** retrying with the unchanged completed file reuses its completed asset and uploads only the failed side

#### Scenario: User replaces only the failed cover
- **WHEN** a video upload is complete and the user replaces an invalid or failed cover
- **THEN** the page preserves the completed video result and creates a new upload identity only for the new cover

#### Scenario: Work creation fails transiently
- **WHEN** both media uploads completed but creating the video returns a transient failure
- **THEN** retrying reuses both completed assets and the same video-creation idempotency identity
