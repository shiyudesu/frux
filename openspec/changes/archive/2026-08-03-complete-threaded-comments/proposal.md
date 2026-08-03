## Why

GCFeed currently exposes only a flat first-page comment list in the Web UI even though the backend already supports basic create, list, and delete operations. A complete short-video discussion experience now needs two-level threads, comment likes, reliable pagination and deletion behavior, plus notifications that return users to the exact discussion that triggered them.

## What Changes

- Replace the flat comment experience with two-level discussions: root comments and chronologically ordered replies, including replies directed at another reply while remaining flattened under one root.
- Add root-comment hot/latest sorting, stable cursor pagination, bounded reply previews, and explicit reply expansion.
- Add comment likes with authenticated viewer state, accurate counters, retry-safe writes, and comment-like notifications.
- Complete comment creation and deletion UX with per-video drafts, reply targeting, character limits, independent busy/error states, permission-aware delete controls, and login guidance.
- Apply differentiated deletion semantics: a commenter's self-deleted root remains as a tombstone when replies exist, while video-author or administrator moderation hides the complete thread.
- Make reply and comment-like notifications actionable by carrying video and comment targets and opening a typed video-detail route focused on the relevant thread.
- Preserve compatibility for existing root-comment API clients while adding endpoints and response fields for replies, likes, sorting, permissions, previews, and deep-link context.
- Migrate existing comments as root comments without losing content, deletion state, counts, or current video visibility protections.

## Capabilities

### New Capabilities

- `threaded-comments`: Defines the two-level comment domain, comment likes, sorting, pagination, previews, creation, deletion, permissions, counters, and migration behavior.
- `actionable-comment-notifications`: Defines reply and comment-like notifications, durable target metadata, self-notification suppression, and navigation to the affected video discussion.

### Modified Capabilities

- `douyin-style-web-experience`: Expands the Feed comment panel and responsive sheet from a flat list/form into a complete threaded interaction surface and adds a typed video-detail discussion destination.
- `web-browser-smoke-testing`: Extends browser coverage to threaded replies, comment likes, pagination, deletion outcomes, responsive expansion, and notification deep links.

## Impact

- Backend interaction domain, application service, PostgreSQL models/repositories, migrations, HTTP DTOs/handlers, router wiring, statistics, hot-score integration, and API-flow/PostgreSQL tests.
- Message domain, persistence, DTOs, message creation adapters, message-center behavior, and notification tests.
- Frontend types, typed hand-written router, comment API module, comment state hook, Feed details panel, video detail page, message navigation, responsive styles, and component/hook/browser tests.
- Product, interaction, message, UI/UX, engineering, and OpenSpec documentation.
- No new routing library or external runtime dependency is introduced.
