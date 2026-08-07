## ADDED Requirements

### Requirement: Separate Protected Cover Inspection
Authorized review readers SHALL be able to inspect the current review subject's protected cover independently from video playback using the same bounded preview authorization and expiry.

#### Scenario: Reviewer opens a subject with a cover
- **WHEN** an authorized reviewer loads the current review detail and protected cover access is available
- **THEN** the Web displays a separately labeled cover inspection surface in addition to using the cover as the video poster

#### Scenario: Cover access expires
- **WHEN** the protected review preview is refreshed before expiry
- **THEN** both video and cover inspection use the refreshed authorized URLs without changing public media state

#### Scenario: Cover is unavailable
- **WHEN** the subject has no resolvable protected cover
- **THEN** the review detail shows an explicit cover-unavailable state while preserving video evidence and review actions

#### Scenario: Review authorization becomes stale
- **WHEN** permission, case version, or subject lifecycle no longer permits preview
- **THEN** both video and cover access are removed and no stale protected cover remains rendered
