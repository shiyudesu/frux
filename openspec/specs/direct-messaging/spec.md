# direct-messaging Specification

## Purpose

Defines private conversations, video sharing, eligibility, unread state, synchronization, and chat telemetry for Frux.

## Requirements

### Requirement: Direct conversations are separate from event notifications
Frux SHALL persist user-authored private conversations and messages in a dedicated chat capability. Existing event-driven notifications SHALL retain their current persistence, delivery, pagination, read state, and deep-link behavior.

#### Scenario: User sends a private message
- **WHEN** an eligible user sends a valid private message
- **THEN** the message is stored in the shared conversation and no `user_message` notification row is used as its message history

#### Scenario: Existing notification is delivered
- **WHEN** an interaction, lifecycle, relation, or account event creates a notification
- **THEN** it continues through the existing notification module without creating a chat conversation

### Requirement: Messaging eligibility requires current mutual follow
The chat capability SHALL allow a new conversation or message only when the authenticated sender and target are distinct normal consumer accounts and each currently follows the other.

#### Scenario: Mutual followers start a conversation
- **WHEN** two normal users actively follow one another and one requests a conversation with the other
- **THEN** Frux returns the canonical existing conversation or creates one conversation containing both users

#### Scenario: Follow is only one directional
- **WHEN** the sender follows the target but the target does not follow the sender
- **THEN** conversation creation and message sending are rejected with a stable ineligible reason

#### Scenario: Participant unfollows after messages exist
- **WHEN** either participant removes their follow relationship
- **THEN** both participants may still read existing history but neither may send another message until mutual follow is restored

#### Scenario: User targets themselves
- **WHEN** a user attempts to create a conversation with their own user ID
- **THEN** the request is rejected and no conversation or member row is created

#### Scenario: Participant account is not normal
- **WHEN** the sender or recipient is frozen, deleted, non-consumer, or otherwise unavailable
- **THEN** the send is rejected without exposing private account-state details to the other participant

### Requirement: One canonical conversation exists per user pair
Frux SHALL canonicalize the two participant IDs and enforce one one-to-one conversation per pair. Creating the same pair repeatedly SHALL return the same conversation, and empty conversations SHALL NOT appear in the normal conversation list.

#### Scenario: Both users request the conversation concurrently
- **WHEN** the two mutually following users concurrently create a conversation with each other
- **THEN** both requests resolve to one conversation containing exactly two member records

#### Scenario: Eligible profile action opens an empty conversation
- **WHEN** a user opens a newly created conversation before sending its first message
- **THEN** the conversation is addressable by its typed route but is absent from ordinary last-message conversation pages

### Requirement: Text message creation is atomic and retry safe
Frux SHALL accept bounded non-empty text messages with a required idempotency key. The message row, conversation last-message state, and recipient unread state SHALL commit atomically.

#### Scenario: Text message succeeds
- **WHEN** an eligible member sends text that remains non-empty after normalization and is within all configured limits
- **THEN** Frux stores one message, advances the conversation last-message identity, increments only the recipient unread count, and returns the committed message

#### Scenario: Same send is retried
- **WHEN** the same sender retries the same normalized conversation, idempotency key, kind, and content
- **THEN** Frux returns the original message without inserting another row or incrementing unread state again

#### Scenario: Idempotency key is reused with different content
- **WHEN** the same sender reuses an idempotency key for a different conversation, message kind, text, or video ID
- **THEN** Frux returns an idempotency conflict and preserves the original message

#### Scenario: Text is empty or oversized
- **WHEN** normalized text is empty or exceeds the domain or request-size limit
- **THEN** the API rejects it with a stable validation error and changes no conversation state

#### Scenario: Persistence fails during send
- **WHEN** any message, conversation-summary, or unread update fails before commit
- **THEN** the transaction rolls back all send effects and exposes an explicit failure

### Requirement: Video messages reference currently public Frux videos
Frux SHALL support a `VIDEO` message containing only a positive Frux video ID. The referenced video SHALL be published, public, media-ready, and readable at send time, and every read SHALL hydrate its current public presentation.

#### Scenario: User shares a public video
- **WHEN** an eligible sender submits a readable public video ID
- **THEN** the conversation stores a video message by ID and the response renders a current video card

#### Scenario: Client submits media presentation fields
- **WHEN** a video-message request attempts to supply a media URL, cover URL, title, author snapshot, or arbitrary metadata
- **THEN** those fields are rejected or ignored according to the strict request schema and are not persisted

#### Scenario: Shared video later becomes unreadable
- **WHEN** a stored video message is listed after the video becomes private, deleted, taken down, processing, or otherwise unreadable
- **THEN** the response contains an explicit unavailable-video card without protected metadata or media URLs

#### Scenario: Video IDs repeat within one page
- **WHEN** a history page contains multiple messages for the same video
- **THEN** current video presentation is hydrated through a bounded batch read rather than one query per message

### Requirement: Internal video sharing lists only eligible recipients
Frux SHALL provide a stable paginated recipient source containing normal users who are currently mutual followers of the authenticated user. The Web SHALL use that source when sharing a Frux video through private messaging.

#### Scenario: User opens the share recipient picker
- **WHEN** an authenticated user activates share for a readable Frux video
- **THEN** the picker lists only current mutual followers with public display fields and optional existing conversation identity

#### Scenario: User filters recipients
- **WHEN** the user submits a valid nickname filter and continues pagination
- **THEN** the normalized filter is bound to the stable cursor and only matching eligible recipients are returned

#### Scenario: User selects one recipient
- **WHEN** the user selects an eligible recipient and confirms share
- **THEN** Frux resolves the canonical conversation and sends one retry-safe `VIDEO` message for the active video

#### Scenario: Share response is uncertain
- **WHEN** conversation creation or video-message sending may have committed but its response is lost
- **THEN** retrying without changing the video or recipient reuses the same operation identities and does not create a duplicate message

#### Scenario: Eligibility changes while picker is open
- **WHEN** the selected user is no longer a mutual follower at confirmation time
- **THEN** the authoritative send is rejected, the picker refreshes eligibility, and no message is created

#### Scenario: User shares from active playback
- **WHEN** sharing succeeds from Feed, video detail, or collection-queue playback
- **THEN** the active playback context remains in place and the user receives confirmation with an explicit option to open the conversation

### Requirement: Conversation lists use stable ordering and current participant display
Frux SHALL list non-empty conversations by descending last-message identity with a stable conversation-ID tie breaker. Each item SHALL contain the counterpart's current public display fields, a safe last-message summary, last-message time, and the authenticated member's unread count.

#### Scenario: User lists conversations
- **WHEN** the authenticated user requests a conversation page
- **THEN** only conversations containing that user and at least one message are returned in stable newest-first order

#### Scenario: New message arrives between pages
- **WHEN** another conversation receives a message while the user paginates with an existing cursor
- **THEN** the bound cursor prevents duplicates or reordering within the continuation page

#### Scenario: Counterpart display changes
- **WHEN** the counterpart updates their nickname or avatar
- **THEN** a later conversation page uses the current public display rather than a message-time account snapshot

#### Scenario: Counterpart is unavailable
- **WHEN** the counterpart can no longer expose their normal public display
- **THEN** the conversation remains readable with a safe unavailable-user fallback and no account identifier

### Requirement: Message history uses stable conversation-bound pagination
Frux SHALL return only messages belonging to the authenticated member's requested conversation, ordered by descending message ID with a cursor bound to that conversation.

#### Scenario: Member loads recent history
- **WHEN** a conversation member requests its first history page
- **THEN** Frux returns the newest bounded messages and a cursor for older history

#### Scenario: Cursor is reused for another conversation
- **WHEN** a client supplies a cursor created for one conversation to another conversation
- **THEN** Frux rejects the cursor without returning either conversation's messages

#### Scenario: Non-member requests history
- **WHEN** an authenticated user requests a conversation they do not belong to
- **THEN** Frux returns a safe not-found or forbidden result without revealing participant or message data

### Requirement: Read progress and chat unread counts are monotonic
Frux SHALL track read progress independently for each conversation member. Marking through a message SHALL never move read progress backward, and chat unread totals SHALL derive from committed member state.

#### Scenario: Recipient reads through the latest message
- **WHEN** a member marks through the latest message currently visible in the conversation
- **THEN** their read boundary advances and the remaining unread count becomes zero

#### Scenario: Recipient marks a partial boundary
- **WHEN** unread messages exist after the accepted through-message
- **THEN** the member retains the exact count of later unread messages

#### Scenario: Older read request arrives late
- **WHEN** a delayed read request names a message older than the member's current read boundary
- **THEN** the request is state-idempotent and does not increase unread count or move the boundary backward

#### Scenario: Message belongs to another conversation
- **WHEN** a read request names a message outside the requested conversation
- **THEN** the request is rejected and no member state changes

### Requirement: Notification and chat unread counts remain distinct
Frux SHALL preserve the existing notification unread endpoint and SHALL provide an additive inbox summary containing notification, chat, and total unread counts.

#### Scenario: User has both unread kinds
- **WHEN** a user has two unread notifications and three unread chat messages
- **THEN** the inbox summary returns notification count two, chat count three, and total count five

#### Scenario: User marks all notifications read
- **WHEN** the user performs the existing notification mark-all-read operation
- **THEN** notification unread becomes zero while chat unread and chat read boundaries remain unchanged

#### Scenario: Navigation badge refreshes
- **WHEN** the authenticated Web client refreshes shared unread state
- **THEN** the primary message badge uses total unread while each message view renders its own unread count

### Requirement: The Web synchronizes conversations without stale cross-session updates
The Web SHALL incrementally refresh visible conversation data, SHALL pause background polling when the workspace is inactive or hidden, and SHALL isolate state by authenticated user and conversation.

#### Scenario: Active conversation receives a new message
- **WHEN** polling discovers a committed message newer than the active conversation's current boundary
- **THEN** the Web appends it once in server order without replacing loaded older history

#### Scenario: User changes conversations during a request
- **WHEN** an old conversation request completes after another conversation becomes active
- **THEN** the old response cannot update the active conversation

#### Scenario: User changes account
- **WHEN** logout, login, or token replacement changes the authenticated user
- **THEN** conversation, draft, polling, and unread state from the previous user are cleared before new data is applied

#### Scenario: Polling fails transiently
- **WHEN** an incremental refresh fails without invalidating authentication
- **THEN** loaded messages remain visible, the client uses bounded retry or backoff, and the interface exposes a truthful degraded-refresh state

### Requirement: Chat telemetry excludes message and identity data
Chat logs, metrics, traces, and monitoring labels SHALL NOT contain message text, nicknames, account identifiers, user IDs, conversation IDs, message IDs, video IDs, or media URLs.

#### Scenario: Send outcome is observed
- **WHEN** the server records chat send latency or outcome
- **THEN** telemetry uses only closed operation, message-kind, outcome, and error-class dimensions

#### Scenario: Chat request fails
- **WHEN** a request fails validation, authorization, persistence, or hydration
- **THEN** repository-standard diagnostics identify the technical failure without logging the private message body or participant identity
