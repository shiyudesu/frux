## Why

Frux currently exposes a notification inbox but has no user-to-user conversation model, so users cannot privately discuss creators or share Frux videos with people they know. Direct messaging is the missing social loop behind issue 42 and provides a natural in-product destination for the still-unimplemented video sharing capability.

## What Changes

- Add durable one-to-one conversations for mutually following users, with text messages and internal video-card messages.
- Keep event-driven notifications in the existing `message` module and introduce a separate `chat` domain for conversations, members, messages, read state, and unread counts.
- Add authenticated conversation-list, history, send, and mark-read APIs with stable cursor pagination and retry-safe message creation.
- Add a paginated eligible-recipient query and connect existing video share actions to a one-recipient private-message picker.
- Require current mutual follow status for every new send; preserve prior history after either side unfollows while preventing further messages.
- Revalidate shared videos when sent and read, and show a safe unavailable card when a video is no longer publicly readable.
- Expand the Web `/messages` destination into separate private-message and notification views, add typed conversation routing, and add a private-message action to eligible public profiles.
- Start with HTTP incremental synchronization backed by PostgreSQL; leave WebSocket delivery, stranger requests, attachments, read receipts, recall, and group chat for later changes.
- Preserve all existing notification APIs, event delivery, notification pagination, and notification deep links.

## Capabilities

### New Capabilities

- `direct-messaging`: One-to-one mutual-follow conversations, text and video-card messages, durable history, read state, unread aggregation, authorization, and Web conversation behavior.

### Modified Capabilities

- `profile-dashboard`: Add a truthful private-message action for authenticated visitors and preserve follow/profile state isolation.
- `douyin-style-web-experience`: Separate private conversations from notifications within the shared message experience while preserving existing notification behavior and desktop responsiveness.

## Impact

- New backend `chat` domain, application service, PostgreSQL models/repository, HTTP DTO/handler, router wiring, migration registration, API-flow tests, and module documentation.
- Narrow adapters to account, relation, and video capabilities for active-account checks, mutual-follow authorization, participant display hydration, and public-video validation.
- New PostgreSQL conversation, member, and message tables with pair uniqueness, message idempotency, stable ordering, member unread state, and supporting indexes.
- New authenticated chat APIs and an additive unread-summary API; existing `/api/messages` and `/api/message-stats/unread` notification contracts remain valid.
- Frontend route/types/API/hooks/pages/components for conversation lists, message history, and the video-share recipient picker, plus updates to the message shell, public-profile actions, Feed/detail/queue share actions, navigation badges, and responsive styles.
- `docs/product.md`, `docs/architecture.md`, `docs/engineering.md`, `docs/uiux.md`, `docs/optimization.md`, `docs/security.md`, `docs/modules/message.md`, a new `docs/modules/chat.md`, and `docs/当前问题.md`.
- No new frontend package, external messaging service, Kafka topic, or attachment upload type is introduced in this change.
