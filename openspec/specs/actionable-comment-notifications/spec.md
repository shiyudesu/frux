# actionable-comment-notifications Specification

## Purpose

Defines durable, deduplicated comment notifications and typed navigation from messages to their discussion targets.

## Requirements

### Requirement: Discussion notifications are generated from durable events
Accepted root comments, replies, and new active comment likes SHALL create retryable notification events in the same durable transaction as their interaction fact. Delivery SHALL be idempotent by recipient and event ID.

#### Scenario: Root comment notifies the video author
- **WHEN** a user other than the video author creates a root comment
- **THEN** the video author eventually receives one `COMMENT` message containing the actor and discussion target

#### Scenario: Reply notifies the direct target
- **WHEN** a user replies to another user's root comment or reply
- **THEN** the direct target author eventually receives one `COMMENT_REPLY` message for that reply

#### Scenario: Comment like notifies the comment author
- **WHEN** a user newly likes another user's active root comment or reply
- **THEN** the comment author eventually receives one `COMMENT_LIKE` message and repeated effective likes do not create duplicates

#### Scenario: Actor would notify themselves
- **WHEN** the video author comments on their own video, a user replies to their own comment, or a user likes their own comment
- **THEN** no self-notification event is created

### Requirement: Comment notification targets are explicit
Comment-related messages SHALL persist `video_id`, `comment_id`, and `root_comment_id` as structured target fields instead of requiring clients to parse IDs from message text or event IDs.

#### Scenario: Message is listed
- **WHEN** the recipient loads a comment, reply, or comment-like message
- **THEN** the response includes the available video, root-comment, and target-comment IDs together with actor and read state

#### Scenario: Legacy message is listed
- **WHEN** an older message has no structured target fields
- **THEN** it remains readable and markable as read without causing a navigation failure

### Requirement: Notification delivery is retryable without user-visible duplication
Failed comment-notification delivery SHALL remain in a leased durable outbox and SHALL retry with bounded backoff until delivered or terminally rejected.

#### Scenario: Message service is temporarily unavailable
- **WHEN** an interaction transaction commits but message creation fails transiently
- **THEN** the accepted comment or like remains successful and the outbox retries delivery without requiring the user to repeat the interaction

#### Scenario: Notification event is redelivered
- **WHEN** the same outbox event or internal message request is processed more than once
- **THEN** the recipient retains exactly one message for that event

### Requirement: Message navigation opens the targeted discussion
The Web message center SHALL treat a comment-related message as an actionable navigation target. Activating it SHALL mark it read and open a typed video-detail route with the relevant root thread expanded and the target comment highlighted when still readable.

#### Scenario: User opens a reply notification
- **WHEN** the recipient activates a `COMMENT_REPLY` message whose video and thread remain public
- **THEN** the application opens that video, loads the specified root thread, expands replies, and brings the target reply into view

#### Scenario: User opens a comment-like notification
- **WHEN** the recipient activates a `COMMENT_LIKE` message for a readable comment
- **THEN** the application opens the containing video and highlights the liked root comment or reply

#### Scenario: Target is no longer available
- **WHEN** the message target was deleted, moderated, or made unreadable
- **THEN** the message is still marked read and the destination presents an explicit unavailable-discussion state without exposing hidden content

### Requirement: Comment notification types render distinctly
The message domain and Web message center SHALL recognize `COMMENT`, `COMMENT_REPLY`, and `COMMENT_LIKE` as valid typed messages with truthful labels and icons.

#### Scenario: Mixed interaction messages are displayed
- **WHEN** a user's message list contains video likes, root comments, replies, and comment likes
- **THEN** each message type renders a distinguishable title, body, actor, and icon while preserving existing pagination and read controls
