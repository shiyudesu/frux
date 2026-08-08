## Why

The current Following Feed uses the same full-stage presentation as every other Feed scene, so users can watch followed-author videos but cannot simultaneously see or navigate the people they follow. Authenticated Chrome DevTools research of Douyin's desktop Following page shows a stable secondary directory beside the independently scrollable video stage, which directly addresses issue 38 without introducing a mobile-only surface.

## What Changes

- Add a Following-only people directory between the main navigation rail and the video stage.
- Keep the directory 208px wide across wide, compact, and narrow desktop layouts, with an explicit collapse or reopen control when users need more video width.
- Show a paginated, searchable list of the current user's active follows with real avatar, nickname, and available profile metadata.
- Make each directory row open the existing public-profile route; selecting a person SHALL NOT silently replace or filter the ordered Following Feed.
- Isolate directory scrolling and search interaction from Feed wheel, swipe, and keyboard shortcuts.
- Add optional query filtering to the authenticated following-list API so the visible search control searches the complete relationship set rather than only the currently loaded page.
- Keep live status, unread-work counts, and similar Douyin-only labels out of Frux until corresponding durable product facts exist.
- Coordinate the directory with comments: preserve push-style comments only when sufficient stage width remains, otherwise use the existing right-side drawer or collapse the directory.
- Preserve Following Feed ordering, cursor pagination, preloading, interactions, view events, and the current relation truth-source guarantees.
- Extend component, API-flow, responsive geometry, accessibility, and screenshot coverage.

## Capabilities

### New Capabilities

- `following-feed-directory`: Defines the authenticated Following Feed people directory, relationship search and pagination, row navigation, collapse behavior, independent scrolling, and integration with the video stage.

### Modified Capabilities

- `douyin-style-web-experience`: Refine the Following Feed desktop composition and responsive comment behavior when the 208px directory is present.
- `web-browser-smoke-testing`: Add wide, compact, narrow, collapsed-directory, search, profile-navigation, scroll-isolation, and comments-open coverage for the Following Feed.

## Impact

- Backend relation query parsing, cursor binding, application service, repository filtering, API tests, and relation documentation.
- Frontend social API types, Following directory state or hook, a new shared component, `FeedPage` composition, responsive Feed CSS, profile navigation, and focused component tests.
- Browser geometry and screenshot verification at 1440px, 1200px, 1024px, and 800px.
- No new external dependency, database table, media behavior, or mobile navigation is required.
