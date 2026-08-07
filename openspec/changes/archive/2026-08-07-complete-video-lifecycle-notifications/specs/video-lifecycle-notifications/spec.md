## ADDED Requirements

### Requirement: Durable Creator Lifecycle Coverage
Frux SHALL create durable creator notification facts for video submission, review rejection, approval that remains media-gated, first public publication, terminal media-processing failure, takedown, and restoration. Transient upload progress, retry attempts, and automatic routing to human review SHALL NOT create message-center entries.

#### Scenario: Creator submits a reviewable video
- **WHEN** video creation commits a new pending-review version
- **THEN** Frux commits a submission notification fact for that creator and review version in the same transaction

#### Scenario: Upload progress changes
- **WHEN** local or direct upload progress advances without completing video creation
- **THEN** the upload page may update transient progress and no durable lifecycle message is created

#### Scenario: Media processing enters a retry
- **WHEN** a retryable probe, transcode, or publication attempt fails
- **THEN** Frux records retry state and does not notify the creator of a terminal failure

#### Scenario: Media processing fails terminally
- **WHEN** the current video's required media baseline reaches terminal failure
- **THEN** Frux creates one durable media-failure notification with a registered safe reason

#### Scenario: Video is taken down or restored
- **WHEN** an audited enforcement or restoration transition commits
- **THEN** Frux commits one corresponding creator notification fact in the same transaction

### Requirement: Truthful Review and Publication Messages
Frux SHALL distinguish review approval from actual public eligibility and SHALL combine them into one publication message only when approval completes every public-availability gate.

#### Scenario: Approval precedes baseline readiness
- **WHEN** a pending video is approved while its required media baseline is not ready
- **THEN** the creator receives an approved-but-processing notification and no published notification

#### Scenario: Approval completes the final gate
- **WHEN** a pending video is approved while its required baseline and public visibility are ready
- **THEN** the creator receives one “审核通过并已发布” publication notification rather than separate simultaneous approval and publication messages

#### Scenario: Baseline completes after approval
- **WHEN** the required baseline becomes ready for an already approved public video
- **THEN** the creator receives one first-publication notification

#### Scenario: Review rejects the video
- **WHEN** an automated or human review rejection commits
- **THEN** the creator receives one rejected notification with a registered safe reason code and no publication message

### Requirement: Structured Idempotent Lifecycle Delivery
Every lifecycle notification SHALL carry a stable event ID, recipient, video ID, review version when applicable, registered stage, registered result, optional safe reason code, and occurrence time, and SHALL create at most one user message per recipient and event.

#### Scenario: Outbox delivery is retried
- **WHEN** a Worker delivers the same lifecycle event after a timeout or restart
- **THEN** the message service returns the existing message without creating a duplicate

#### Scenario: Two modules observe the same publication edge
- **WHEN** review and media processing race to submit the same stable publication event
- **THEN** the creator retains exactly one publication message for that video review version

#### Scenario: Lifecycle payload is invalid
- **WHEN** an internal caller submits an unknown stage/result or an oversized identifier or reason
- **THEN** the message service rejects it, the outbox marks a bounded terminal error, and no malformed message is stored

#### Scenario: Message is listed
- **WHEN** a lifecycle message is returned by the public message API
- **THEN** it includes additive structured video ID, stage, result, and safe reason fields without requiring the client to parse title or content

### Requirement: Lifecycle Message Web Behavior
The Web message center SHALL render lifecycle messages from structured fields, mark an activated message read before navigation, and route to a safe creator or public destination according to the current target availability.

#### Scenario: Creator opens a published notification
- **WHEN** the target remains publicly readable
- **THEN** the Web client marks the message read and navigates to the typed video detail route

#### Scenario: Creator opens a protected-state notification
- **WHEN** the message describes submitted, approved-but-processing, rejected, failed, or taken-down work
- **THEN** the Web client marks it read and navigates to the creator works surface rather than attempting anonymous public playback

#### Scenario: Target is no longer available
- **WHEN** the referenced video is deleted or cannot be resolved
- **THEN** the message remains readable and the Web client shows a safe unavailable-target state without leaking protected media

#### Scenario: Legacy review message is listed
- **WHEN** an older unstructured `SYSTEM` review message is returned
- **THEN** it remains readable through the existing compatibility renderer
