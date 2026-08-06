## MODIFIED Requirements

### Requirement: Review-Gated Video Creation
New videos SHALL enter a pending-review lifecycle state, SHALL have no publication time until an approval transition occurs, and SHALL commit a stable creator submission notification fact with the new review version.

#### Scenario: Creator submits a new processing video
- **WHEN** a valid authenticated creator creates a video backed by a new media asset
- **THEN** the video is returned as pending review, is not publicly readable, and its submission notification fact commits atomically

#### Scenario: Legacy-compatible creation path is used
- **WHEN** a valid video is created through the compatibility media path
- **THEN** it follows the same pending-review lifecycle and creates the same versioned submission notification instead of publishing immediately

#### Scenario: Submission transaction fails
- **WHEN** the video or required submission notification fact cannot be persisted
- **THEN** neither the new video nor a success-shaped notification commits

### Requirement: Explicit Review Transitions
The video domain SHALL provide idempotent state-validated transitions for approval, rejection, takedown, and eligible restoration, and each committed creator-visible transition SHALL persist its stable lifecycle notification fact in the same transaction.

#### Scenario: Pending video is approved before media readiness
- **WHEN** an authorized review decision approves a pending-review video whose required baseline is not ready
- **THEN** the video becomes published, receives its publication time exactly once, and commits an approved-but-processing notification fact

#### Scenario: Pending video is approved with every public gate ready
- **WHEN** an authorized review decision approves a pending-review video whose baseline and public visibility are ready
- **THEN** the video becomes published and commits one combined approved-and-published notification fact

#### Scenario: Pending video is rejected
- **WHEN** an authorized review decision rejects a pending-review video
- **THEN** the video becomes rejected, remains unavailable to public reads, and commits one safe rejection notification fact

#### Scenario: Published video is taken down
- **WHEN** an authorized audited enforcement transition takes an eligible video offline
- **THEN** the lifecycle change and stable takedown notification fact commit atomically

#### Scenario: Eligible video is restored
- **WHEN** an authorized audited restoration transition restores an approved offline video
- **THEN** the lifecycle change and stable restoration notification fact commit atomically

#### Scenario: Deleted video receives a transition
- **WHEN** an approval, rejection, takedown, or restoration targets a deleted video
- **THEN** Frux rejects the transition, leaves the deleted state unchanged, and creates no lifecycle notification

### Requirement: Combined Public Eligibility
A video SHALL be publicly readable only when it is published, public in visibility, and backed by a public-ready media baseline. Frux SHALL create at most one first-publication notification per video review version when that combined eligibility first becomes true.

#### Scenario: Review passes before media processing
- **WHEN** a video is approved while its required media baseline is still processing
- **THEN** it remains absent from public reads until the baseline becomes ready and no publication notification is created yet

#### Scenario: Media becomes ready before review
- **WHEN** a pending-review video's baseline becomes ready before approval
- **THEN** it remains absent from public reads until the review transition publishes it and no publication notification is created yet

#### Scenario: Rejected identifier remains cached
- **WHEN** a stale Feed, search, recommendation, or media cache references a rejected video
- **THEN** response assembly and media authorization omit the video and its protected media

#### Scenario: Every public gate becomes ready
- **WHEN** status, visibility, and required baseline transition from not jointly eligible to jointly eligible
- **THEN** public reads may return the video and Frux creates the stable first-publication notification if it has not already been recorded for that review version

#### Scenario: Public eligibility is recalculated
- **WHEN** reconciliation or a repeated lifecycle event observes an already-notified public video review version
- **THEN** Frux does not create another first-publication notification
