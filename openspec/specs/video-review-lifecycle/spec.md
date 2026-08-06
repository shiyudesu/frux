# video-review-lifecycle Specification

## Purpose

Defines review lifecycle states, transitions, public eligibility, and compatibility for video publication.

## Requirements

### Requirement: Review-Gated Video Creation
New videos SHALL enter a pending-review lifecycle state and SHALL have no publication time until an approval transition occurs.

#### Scenario: Creator submits a new processing video
- **WHEN** a valid authenticated creator creates a video backed by a new media asset
- **THEN** the video is returned as pending review and is not publicly readable

#### Scenario: Legacy-compatible creation path is used
- **WHEN** a valid video is created through the compatibility media path
- **THEN** it follows the same pending-review lifecycle instead of publishing immediately

### Requirement: Explicit Review Transitions
The video domain SHALL provide idempotent state-validated transitions for approval, rejection, takedown, and eligible restoration.

#### Scenario: Pending video is approved
- **WHEN** an authorized review decision approves a pending-review video
- **THEN** the video becomes published and receives its publication time exactly once

#### Scenario: Pending video is rejected
- **WHEN** an authorized review decision rejects a pending-review video
- **THEN** the video becomes rejected and remains unavailable to public reads

#### Scenario: Deleted video receives a transition
- **WHEN** an approval, rejection, takedown, or restoration targets a deleted video
- **THEN** Frux rejects the transition and leaves the deleted state unchanged

### Requirement: Combined Public Eligibility
A video SHALL be publicly readable only when it is published, public in visibility, and backed by a public-ready media baseline.

#### Scenario: Review passes before media processing
- **WHEN** a video is approved while its required media baseline is still processing
- **THEN** it remains absent from public reads until the baseline becomes ready

#### Scenario: Media becomes ready before review
- **WHEN** a pending-review video's baseline becomes ready before approval
- **THEN** it remains absent from public reads until the review transition publishes it

#### Scenario: Rejected identifier remains cached
- **WHEN** a stale Feed, search, recommendation, or media cache references a rejected video
- **THEN** response assembly and media authorization omit the video and its protected media

### Requirement: Existing Published Content Compatibility
Existing persisted published videos SHALL remain approved and readable according to their current visibility and media readiness without generating historical review work.

#### Scenario: Migration encounters published video
- **WHEN** the review lifecycle migration runs over an existing published video
- **THEN** its status and publication time remain unchanged
