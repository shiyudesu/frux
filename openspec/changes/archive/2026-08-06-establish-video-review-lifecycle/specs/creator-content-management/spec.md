## ADDED Requirements

### Requirement: Review-Aware Creator Management
Creator management responses and aggregate statistics SHALL represent pending-review and rejected videos without treating them as public works or allowing visibility changes to bypass lifecycle review.

#### Scenario: Creator queries pending review works
- **WHEN** an authenticated creator queries their non-deleted videos
- **THEN** pending-review and rejected owned videos can be returned with truthful lifecycle status

#### Scenario: Pending public-visible video is counted
- **WHEN** a video has public visibility but remains pending review
- **THEN** it is excluded from `public_work_count`

#### Scenario: Creator changes pending video visibility
- **WHEN** the owner changes a pending-review video between public and private visibility
- **THEN** the lifecycle remains pending review and the video does not become publicly readable

#### Scenario: Rejected video is made public
- **WHEN** the owner requests public visibility for a rejected video
- **THEN** Frux rejects the operation or retains the rejected public-ineligible state without publishing it
