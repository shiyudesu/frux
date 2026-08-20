## 1. Chat Domain and Contracts

- [x] 1.1 Add chat message-kind, eligibility-reason, validation-limit, cursor, conversation, member, message, recipient, and hydrated video-card domain types.
- [x] 1.2 Add domain constructors and restoration functions for canonical user pairs, text messages, video messages, monotonic read state, and unavailable participant/video fallbacks.
- [x] 1.3 Add closed chat domain errors for invalid identity, self conversation, ineligible relationship, unavailable account, non-member access, invalid cursor, invalid message shape, unavailable video, and idempotency conflict.
- [x] 1.4 Define chat repository interfaces for conversation creation, eligibility lookup, conversation listing, history listing, atomic send, monotonic mark-read, unread aggregation, and unread reconciliation.
- [x] 1.5 Define narrow application capability interfaces for account status/display hydration, mutual-follow authorization and recipient discovery, public-video validation/hydration, and optional committed-message observation.
- [x] 1.6 Add focused domain tests for pair canonicalization, text normalization and limits, message-kind invariants, cursor identity, and read-boundary monotonicity.

## 2. PostgreSQL Persistence and Migration

- [x] 2.1 Add `chat_conversation`, `chat_conversation_member`, and `chat_message` GORM models with explicit table names, foreign-key fields, timestamps, reserved member/message state, and canonical pair columns.
- [x] 2.2 Add unique and query indexes for canonical user pairs, conversation membership, sender idempotency keys, conversation message order, last-message conversation order, and unread member lookup.
- [x] 2.3 Register all chat models in the shared advisory-lock migration and add migration assertions without modifying or backfilling `user_message`.
- [x] 2.4 Implement naturally idempotent conversation creation that creates exactly two members and resolves concurrent pair creation to one conversation.
- [x] 2.5 Implement conversation lookup and stable non-empty conversation pagination ordered by last message and conversation ID.
- [x] 2.6 Implement stable conversation-bound message-history pagination ordered by descending message ID.
- [x] 2.7 Implement the atomic send transaction with deterministic member locking, sender idempotency replay/conflict checks, message insertion, last-message update, and recipient unread increment.
- [x] 2.8 Implement monotonic mark-read persistence with conversation membership validation, through-message validation, remaining-unread recomputation, and state-idempotent replay.
- [x] 2.9 Implement chat unread aggregation and fact-based member unread reconciliation suitable for startup repair tests and explicit operational use.
- [x] 2.10 Add PostgreSQL integration tests for concurrent conversation creation, concurrent sends, duplicate responses, same-key conflicts, transaction rollback, message ordering, partial reads, stale reads, and reconciliation.

## 3. Chat Application Services

- [x] 3.1 Implement versioned conversation, history, and recipient cursor encoding/decoding with conversation or normalized-query binding and limit normalization.
- [x] 3.2 Implement eligibility lookup and create-or-get conversation use cases with self, account-status, role, and mutual-follow validation.
- [x] 3.3 Implement eligible-recipient listing with bounded nickname search, stable pagination, current public display hydration, and optional existing conversation IDs.
- [x] 3.4 Implement text-message sending with required idempotency keys, strict normalization, authorization recheck, stable errors, and replay-safe results.
- [x] 3.5 Implement video-message sending with current published/public/media-ready validation and persistence of only the video ID.
- [x] 3.6 Implement conversation-list assembly with batch counterpart display hydration, safe unavailable-user fallback, last-message summaries, and member unread counts.
- [x] 3.7 Implement history assembly with bounded batch video hydration and explicit unavailable-video responses that contain no protected presentation data.
- [x] 3.8 Implement monotonic mark-read and combined inbox unread-summary use cases while preserving notification-only unread behavior.
- [x] 3.9 Add chat application metrics hooks using only closed operation, kind, outcome, latency, and error-class dimensions.
- [x] 3.10 Add service unit tests with fakes covering eligibility races, account failures, cursor misuse, bounded hydration, idempotency, unread separation, and content-free telemetry.

## 4. HTTP Surface, Adapters, and Composition

- [x] 4.1 Add strict chat request/response DTOs for eligibility, recipients, conversations, history, text/video send, read progress, and inbox unread summary.
- [x] 4.2 Add chat handlers with authenticated user extraction, positive path-ID parsing, bounded limits, strict JSON binding, required `Idempotency-Key`, and stable API error mapping.
- [x] 4.3 Register `GET /api/chat/users/{targetUserId}/eligibility`, `GET /api/chat/recipients`, `POST /api/chat/conversations`, conversation-list/history, send, mark-read, and `GET /api/inbox-stats/unread` routes.
- [x] 4.4 Add and register a chat-send request-rate policy through the existing layered rate-limit system with explicit unavailable behavior.
- [x] 4.5 Extend the relation repository/application boundary with one-query mutual-follow authorization and stable mutual-recipient pagination rather than intersecting bounded lists in the client.
- [x] 4.6 Add account adapters for normal consumer status and batch current public display fields without exposing canonical account identifiers.
- [x] 4.7 Add video adapters for send-time public readability and batch current card hydration without returning protected URLs for unavailable videos.
- [x] 4.8 Wire repository, services, adapters, observer, metrics, rate limiter, handler, and routes in the HTTP composition root without making Kafka or Redis a send dependency.
- [x] 4.9 Add API-flow tests for eligibility, recipient search, canonical conversation creation, text/video sends, list/history cursors, reads, unread summary, authorization failure, frozen accounts, unfollowed conversations, and notification regressions.

## 5. Frontend Types, APIs, Routing, and State

- [x] 5.1 Add strict TypeScript chat types for eligibility, recipients, conversations, messages, unavailable video cards, pages, send requests/results, read results, and inbox unread summary.
- [x] 5.2 Add a typed `api/chat.ts` module for all chat endpoints and update the existing message API only where the additive inbox summary is consumed.
- [x] 5.3 Extend the hand-written router with validated `/messages/{conversationId}` parsing and typed message-workspace navigation without adding a routing dependency.
- [x] 5.4 Add isolated conversation-list state with initial load, refresh, pagination, deduplication, stable selection, error, and stale-session guards.
- [x] 5.5 Add isolated active-history state with older-page loading, `after_message_id` incremental refresh, server-order deduplication, local send reconciliation, stale-conversation guards, and conversation/eligibility metadata consumption for empty-route reloads.
- [x] 5.6 Add visibility- and route-aware polling with centralized bounded intervals, transient backoff, pause/resume behavior, and cleanup on account or route changes.
- [x] 5.7 Replace the shared navigation badge fetch with the additive inbox summary while retaining separate notification and chat unread counts in context.
- [x] 5.8 Add retry-safe client operation identity management for conversation creation, text sends, video shares, and uncertain responses, retaining identities until success or input/session change.

## 6. Message Workspace UI

- [x] 6.1 Refactor `/messages` into a shared private-message/notification workspace while keeping notifications as the backward-compatible default and preserving the existing notification component behavior.
- [x] 6.2 Build the conversation column with search, stable pagination, current counterpart display, last-message summary, timestamp, unread badge, loading, empty, error, and refresh states.
- [x] 6.3 Build the selected-conversation header and scrollable chronological history with older-history loading, day/time presentation, sender alignment, and safe unavailable-user behavior.
- [x] 6.4 Build the bounded text composer with authentication, eligibility, character limit, keyboard submission, explicit busy/error/retry states, and duplicate-submit prevention.
- [x] 6.5 Build the private-message video card renderer with current cover/title/author presentation, typed navigation for readable videos, and an unavailable-video tombstone.
- [x] 6.6 Mark read through the latest visible received message without moving read state backward, and reconcile conversation and shared unread badges after success.
- [x] 6.7 Preserve loaded history when a conversation becomes ineligible, disable the composer, and show truthful mutual-follow guidance.
- [x] 6.8 Add wide list/detail and compact/narrow list-or-detail layouts that preserve the Frux desktop rail, viewport-safe scrolling, accessible focus, Escape/back behavior, and reduced motion.

## 7. Public Profile and Video Sharing

- [x] 7.1 Load private-message eligibility independently on public profiles with user-ID generation guards and no self-message action.
- [x] 7.2 Add eligible, ineligible, loading, and failure presentations for the public-profile private-message action and resolve the canonical conversation on activation.
- [x] 7.3 Build a reusable private-share recipient dialog with active-video context, eligible-recipient nickname filtering, stable pagination, one-recipient selection, and accessible dismissal/focus return.
- [x] 7.4 Connect Feed, video-detail, and collection-queue share actions to the recipient dialog without interrupting active playback merely to open or close the dialog.
- [x] 7.5 Implement retry-safe video sharing that resolves the conversation, sends one `VIDEO` message, refreshes eligibility after authoritative rejection, and rotates operation identities only after success or input change.
- [x] 7.6 Add share success feedback and an explicit typed action to open the target conversation while preventing duplicate confirmation sends.

## 8. Frontend Verification

- [x] 8.1 Add router and API tests for valid/invalid conversation routes, strict response typing, stable errors, and request payloads.
- [x] 8.2 Add hook/state tests for pagination deduplication, conversation/eligibility metadata, incremental polling, hidden-document pause, transient backoff, account replacement, conversation replacement, and uncertain-send replay.
- [x] 8.3 Add dedicated message-workspace/history tests for notification compatibility, conversation selection, history loading, unread reconciliation, composer eligibility, unavailable videos, accessibility, and responsive desktop modes.
- [x] 8.4 Add dedicated public-profile tests for eligible/ineligible/self states, stale eligibility responses, create-conversation failure, and successful typed navigation.
- [x] 8.5 Add dedicated share-dialog/focus-stack tests for recipient filtering/pagination, dismissal focus, unauthenticated routing, video/recipient changes, uncertain retries, duplicate prevention, preserved playback context, and topmost Escape handling.

## 9. Documentation and Validation

- [x] 9.1 Create `docs/modules/chat.md` covering responsibilities, APIs, tables, authorization, idempotency, pagination, unread semantics, polling, privacy, sharing, tests, and deferred capabilities.
- [x] 9.2 Update `docs/modules/message.md` to state that it owns event notifications only and document the separate combined unread summary.
- [x] 9.3 Update product, architecture, engineering, UI/UX, optimization, and security documentation for the chat module, message workspace, polling/load model, private-content handling, and video sharing.
- [x] 9.4 Mark current-problem items 12 and 42 resolved only after their specified API, Web, sharing, and regression tests pass.
- [x] 9.5 Run targeted chat domain/application/HTTP/PostgreSQL tests, existing message and relation regressions, and the relevant API-flow tests; record that PostgreSQL integration is gated by `FRUX_POSTGRES_TEST_DSN` and was skipped without starting a service when unset.
- [x] 9.6 Run targeted Web chat/history/profile/share/dialog tests, then the strict frontend production build.
- [x] 9.7 Validate Compose configuration if router/config/rate-limit wiring changes deployment configuration.
- [x] 9.8 Run `openspec validate --all --strict` and confirm all `add-direct-messaging` artifacts and implementation checkboxes are consistent before completion.
