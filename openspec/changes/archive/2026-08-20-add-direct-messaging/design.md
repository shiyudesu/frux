## Context

Frux currently has a `message` module that persists event-driven notifications in `user_message`. Those rows belong to one recipient, require a title and body, expose one read flag, and are created by interaction, video, review, relation, and account workflows. The Web `/messages` page renders that notification stream and the shared session context refreshes its unread count on route changes.

Direct messaging has different ownership and consistency needs: one conversation is shared by two participants, messages are ordered within that conversation, read progress is per participant, new sends must be retry safe, and a conversation list must expose a current counterpart, last-message summary, and unread count. Reusing `user_message` would mix internal event delivery with user-authored content and make conversation ordering, authorization, recall, and future real-time delivery difficult.

The current relation module can read one directional follow state, the account module owns current account status and public display data, and the video module owns public readability. The backend has no WebSocket or SSE surface. PostgreSQL is the durable source of truth; Redis and Kafka must not become a prerequisite for accepting a private message.

The Douyin Web reference separates notifications from private conversations, presents a compact conversation list with search and unread state, and uses an independent IM runtime. Frux will adopt the separation and conversation semantics without copying proprietary assets or attempting feature parity in the first release.

## Goals / Non-Goals

**Goals:**

- Add durable one-to-one conversations between currently mutually following, normal consumer accounts.
- Support bounded text messages and internal Frux video cards.
- Provide retry-safe sending, stable history pagination, conversation-list pagination, monotonic read progress, and separate chat unread counts.
- Keep notification persistence, delivery, APIs, deep links, and read state unchanged.
- Add a typed Web conversation workspace that preserves the existing notification view and desktop layout rules.
- Keep PostgreSQL authoritative and use bounded HTTP incremental synchronization for the initial release.
- Establish interfaces that can later support WebSocket wakeups, stranger requests, recall, attachments, and groups without rewriting stored history.

**Non-Goals:**

- Stranger-message requests, follower-only messages, enterprise messaging, automated replies, or contact discovery.
- Group chat, images, files, voice, stickers, reactions, typing indicators, presence, peer read receipts, edit, recall, local deletion, or message search.
- End-to-end encryption or a new external messaging provider.
- Reusing Kafka as the message store or adding a new Kafka topic for accepted chat writes.
- Reusing upload sessions for chat attachments.
- Automatically synthesizing conversations from historical follow or notification data.

## Decisions

### 1. Introduce a separate `chat` module

The backend will add `internal/domain/chat`, `internal/application/chat`, `internal/infra/persistence/chat`, and `internal/interfaces/http/chat`. The existing `message` module remains the notification domain.

The chat application service will depend on narrow capabilities for:

- current participant account status and batch display profiles;
- mutual-follow authorization;
- paginated mutually followed recipient discovery;
- public video validation and batch video-card hydration;
- an optional delivery observer reserved for later real-time wakeups.

Composition-root adapters will connect those capabilities without importing account, relation, or video infrastructure into the chat domain.

**Alternative considered:** Add `DIRECT_MESSAGE` to `user_message`. Rejected because notification rows have one recipient and event identity, while chat needs shared conversation identity, per-member state, user-authored idempotency, and ordered history.

### 2. Use three PostgreSQL tables

`chat_conversation` stores:

- `id`;
- canonical `lower_user_id` and `higher_user_id`;
- nullable `last_message_id`;
- nullable `last_message_at`;
- `created_at` and `updated_at`;
- a unique constraint on `(lower_user_id, higher_user_id)`.

`chat_conversation_member` stores:

- `(conversation_id, user_id)` primary identity;
- `last_read_message_id`;
- `last_read_at`;
- denormalized `unread_count`;
- reserved nullable `muted_at` and `hidden_at`;
- timestamps.

Exactly two member rows are created with the conversation. Reserved member columns do not expose mute or hide controls in this change, but avoid replacing the membership model when those capabilities arrive.

`chat_message` stores:

- globally increasing `id`;
- `conversation_id` and `sender_id`;
- closed message kind `TEXT` or `VIDEO`;
- normalized text or nullable `video_id`;
- required `idempotency_key`;
- nullable `revoked_at` reserved for a future recall change;
- server `created_at`;
- a unique constraint on `(sender_id, idempotency_key)`;
- an index on `(conversation_id, id DESC)`.

Message IDs provide stable ordering and read boundaries. Server timestamps are presentation data and are not trusted for ordering.

**Alternative considered:** Store participants as an array or JSON field on the conversation. Rejected because member read state, unread counts, future mute settings, and indexed membership queries require relational rows.

### 3. Create or retrieve conversations by canonical user pair

`POST /api/chat/conversations` accepts a positive `target_user_id`. The service rejects self-conversations, validates both accounts, verifies current mutual follow, canonicalizes the pair, and returns the existing row or creates the conversation and both members. Empty conversations are excluded from the normal conversation list until their first message.

`GET /api/chat/users/{targetUserId}/eligibility` returns whether the current user may start or continue messaging, a closed reason code when not eligible, and an existing conversation ID when one exists. This endpoint lets the public profile render a truthful action without exposing either user's account identifier or raw relationship rows.

Conversation creation remains naturally idempotent through the unique pair constraint.

`GET /api/chat/recipients?q={nickname}&cursor={cursor}&limit={limit}` returns normal users who currently have both active follow directions with the caller. Results use stable relationship-time and user-ID ordering, optional bounded nickname search, current public display fields, and an optional existing conversation ID. It is the only recipient source for the initial share picker; the client does not infer eligibility by intersecting partially loaded following and follower lists.

### 4. Authorize every send against current facts

Every send verifies:

- authenticated sender identity;
- sender and recipient are different normal consumer accounts;
- both active follow directions exist at the authorization read;
- the sender belongs to the conversation;
- the message shape is valid.

If either side unfollows, history remains readable to both members but subsequent sends fail with a stable chat-not-eligible conflict. A later mutual follow allows sending in the same conversation again.

The relation capability will read both directions in one bounded query. A follow change committed after that authorization read affects the next send; the design does not hold relationship locks across modules.

### 5. Commit message, conversation summary, and unread state atomically

`POST /api/chat/conversations/{conversationId}/messages` requires `Idempotency-Key` and a closed body:

```json
{ "kind": "TEXT", "text": "hello" }
```

or:

```json
{ "kind": "VIDEO", "video_id": 123 }
```

Text is trimmed, must remain non-empty, and is bounded by domain constants for Unicode code points and encoded request size. A video message contains only a positive video ID and no client-supplied media URL, title, cover, or author snapshot.

The repository transaction:

1. locks or creates the canonical conversation and member rows;
2. resolves an existing message by sender and idempotency key;
3. rejects a same-key/different-payload replay;
4. inserts the message;
5. updates the conversation's last-message identity and time;
6. increments only the recipient member's unread count;
7. returns the committed message.

Member rows are locked in deterministic user-ID order. A replay returns the original message and does not increment unread state again.

**Alternative considered:** Publish a Kafka event before saving the message. Rejected because broker availability and acknowledgement uncertainty must not determine whether a private message exists.

### 6. Store video references and hydrate current cards

The video capability validates at send time that a referenced video is currently published, public, and media-ready. The message stores only `video_id`.

Conversation-history reads batch unique video IDs and request current public cards through a narrow video capability. If a video is deleted, private, taken down, processing, or otherwise unreadable, the response contains an explicit unavailable-video representation without a media URL, cover URL, title, or protected metadata.

This prevents old chat responses from bypassing current video visibility and avoids persisting expiring signed URLs.

### 7. Use stable cursor pagination and bounded hydration

Conversation lists sort by `(last_message_id DESC, conversation_id DESC)` and exclude empty conversations. Their cursor binds both values.

Message history sorts by `message_id DESC`; the API returns newest-first pages with a cursor bound to the last returned message ID. The Web reverses or prepends pages for chronological display without offset pagination.

Participant profiles and video cards are batch hydrated per page. Account or media disappearance uses safe fallback presentation rather than N+1 reads or hidden-data leakage.

### 8. Track monotonic read progress separately from notifications

`PATCH /api/chat/conversations/{conversationId}/read` accepts `through_message_id`. The repository verifies that the message belongs to the conversation, locks the member row, refuses to move `last_read_message_id` backward, and recomputes the remaining unread messages after the accepted boundary. Replays are state-idempotent.

The existing `/api/message-stats/unread` remains notification-only. A new `/api/inbox-stats/unread` returns:

```json
{
  "notification_unread_count": 2,
  "chat_unread_count": 3,
  "total_unread_count": 5
}
```

The shared navigation badge uses the total, while notification and chat views use their own counts. Marking all notifications read never clears chat unread state.

### 9. Start with incremental HTTP synchronization

The initial Web implementation uses bounded, visibility-aware polling:

- conversation-list refresh while the authenticated message workspace is visible;
- `after_message_id` incremental reads for the active conversation;
- immediate local reconciliation after a successful send or read update;
- pause while the document is hidden, the route is inactive, or the session changes;
- request generation guards so stale responses cannot update another user or conversation.

Polling intervals are centralized constants with backoff after transient failure. PostgreSQL remains sufficient for correctness if a refresh is delayed.

An optional future delivery observer can publish only committed conversation/message identities through Redis Pub/Sub to WebSocket-connected API instances. Such wakeups will be hints; clients will still fetch authoritative rows through HTTP.

**Alternative considered:** Introduce WebSocket delivery immediately. Rejected because Frux has no existing real-time lifecycle, multi-instance fanout, reconnect protocol, or operational metrics, and correctness does not require it for the first release.

### 10. Expand the existing Web message destination

`/messages` remains valid and defaults to the current notification view for backward compatibility. The message workspace adds private-message and notification tabs.

Typed `/messages/{conversationId}` routes open the private-message workspace with the selected conversation. The chat layout uses:

- a bounded conversation column inspired by the compact Douyin list;
- a conversation header and scrollable history;
- a bottom composer for text;
- a safe video-card renderer;
- loading, empty, polling-degraded, send-busy, retry, ineligible, and unavailable-video states.

Wide desktop renders list and detail together. Compact and narrow desktop retain the existing Frux desktop shell; the selected conversation may replace the list with an explicit back control rather than introducing a mobile bottom-navigation mode.

Public profiles request chat eligibility independently of follow-state loading. Eligible visitors get a private-message action; ineligible visitors see truthful mutual-follow guidance rather than a control that fails only after composition.

### 11. Route existing video share actions through an eligible-recipient picker

The existing share actions on Feed, video detail, and collection-queue playback will open a Frux-owned private-share dialog for the active readable video. The dialog:

- queries `/api/chat/recipients` rather than exposing arbitrary user search;
- supports bounded nickname filtering and stable pagination;
- allows one recipient per send in the initial release;
- creates or retrieves the canonical conversation;
- sends a `VIDEO` message with a stable idempotency key;
- retains the same conversation and message keys while retrying an uncertain response;
- rotates identities after success, recipient change, or video change;
- reports explicit eligibility, unavailable-video, send, and retry states.

Successful sharing provides confirmation and may navigate to the conversation through an explicit action; it does not automatically interrupt the current Feed or collection playback. The Web Share API and external links are not required for this internal capability.

**Alternative considered:** Reuse global user search as the recipient picker. Rejected because search includes users who are not eligible to receive a message and would make the client reconstruct authorization from incomplete relationship pages.

### 12. Protect content and metadata

- API errors use stable chat error codes and safe user-facing text.
- Logs, metrics, traces, and Prometheus labels never contain message text, participant IDs, conversation IDs, video IDs, nicknames, or media URLs.
- Metrics aggregate only operation, message kind, outcome, and bounded latency/error classes.
- Message bodies remain in PostgreSQL and backups; this change adds no search index, analytics export, or Kafka copy.
- Public profile and chat responses never expose canonical account identifiers.
- Send endpoints use the existing authenticated-user rate-limit system with a chat-specific policy and fail explicitly when the configured limiter is unavailable.

## Risks / Trade-offs

- **[Polling delays new messages]** → Refresh only active views, reconcile immediately after local actions, expose degraded refresh state, and keep the transport interface replaceable.
- **[Unread counters drift after a bug or interrupted migration]** → Update message and member state in one transaction and provide a fact-based reconciliation function for tests, migration repair, and operations.
- **[Mutual-follow state changes immediately after authorization]** → Define authorization at the bounded relationship read; every later send rechecks current state.
- **[Empty conversation rows accumulate]** → Exclude them from lists and allow later cleanup of old rows with no messages; do not synthesize them during migration.
- **[A shared video becomes unavailable]** → Store only the ID and hydrate current visibility into an unavailable tombstone.
- **[Direct messages create moderation and abuse expectations]** → Limit v1 to mutual follows, add endpoint rate limits, preserve explicit future boundaries for block/report, and do not open stranger messaging in this change.
- **[Chat content increases sensitive backup data]** → Avoid secondary copies and content-bearing telemetry; document backup and future retention requirements.
- **[Notification and chat badges become confusing]** → Keep separate counts and use only the additive inbox summary for the combined navigation badge.

## Migration Plan

1. Add chat models to the shared migration transaction and create indexes without touching `user_message`.
2. Add repositories, services, adapters, handlers, rate-limit registration, reconciliation, tests, and documentation.
3. Deploy backend APIs first; no existing client calls them and empty tables require no backfill.
4. Deploy the Web message workspace, profile eligibility action, typed routes, polling, and additive unread summary.
5. Enable the private-share picker on Feed, detail, and collection-queue share actions after chat send flows are available.
6. Observe bounded operation/error/unread metrics and compare member unread counters with reconciliation results.
7. Mark issues 12 and 42 resolved only after text, video-card sharing, history, unread, eligibility, and notification-regression flows pass.

Rollback removes the Web entry points and stops chat API traffic. The new tables remain unused and are not automatically dropped; existing notification behavior continues unchanged.

## Open Questions

- Define a later retention and user-visible deletion policy before adding export, deletion, or legal-hold workflows.
- Define cross-product block/report semantics before allowing strangers or non-mutual followers to message.
- Introduce WebSocket delivery only after polling load and message-latency metrics justify the additional runtime.
