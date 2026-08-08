## 1. Relationship Search API

- [x] 1.1 Extend relation domain list inputs, user items, and cursor metadata with additive account output, normalized query binding, list kind, and cursor version while retaining empty-query legacy cursor compatibility.
- [x] 1.2 Update the PostgreSQL relation repository to search only active follows to normal accounts by parameterized account or nickname matching while preserving stable `updated_at DESC, target_user_id DESC` pagination.
- [x] 1.3 Update the relation application service and HTTP handler to validate `q`, bind cursors to the normalized query and list kind, and return the additive account field without changing existing empty-query callers.
- [x] 1.4 Add API-flow and repository tests for account or nickname search, active-account filtering, pagination, legacy empty-query cursors, cross-query or cross-kind cursor rejection, invalid query limits, and anonymous rejection.
- [x] 1.5 Update `docs/modules/relation.md` with the searchable following-list contract, cursor compatibility, response field, visibility rules, and test scenarios.

## 2. Following Directory Data State

- [x] 2.1 Extend Web relation types and API helpers with the account field and optional query parameter while preserving the profile relation modal's existing calls.
- [x] 2.2 Add `useFollowingDirectory` with debounced normalized search, independent cursor state, ID deduplication, loading or loading-more or empty or error states, request-generation stale-response protection, pagination, retry, and mutation patching.
- [x] 2.3 Add hook tests for initial load, query reset, stale response rejection, deduplicated pagination, partial-page failure, empty state, and row removal after unfollow.

## 3. Following Directory Presentation

- [x] 3.1 Add `FollowingFeedDirectory` with a title, functional search, truthful identity rows, loading or empty or error states, reachable load-more or retry controls, typed public-profile navigation, and no live or unread placeholder UI.
- [x] 3.2 Add accessible collapse and reopen controls that preserve directory query, rows, cursor, and scroll position without reloading or changing the active Feed item.
- [x] 3.3 Integrate the directory as a sibling of `feed-main` only for the authenticated Following scene and keep directory wheel, pointer, and editable input behavior outside Feed navigation handlers.
- [x] 3.4 Apply successful Follow or unfollow mutations to both the existing author-follow map and mounted directory state without allowing stale directory responses to restore removed users.
- [x] 3.5 Add the 208px directory token and Following grid variants for wide, compact, and narrow desktop layouts, including stage-edge reopen placement and no horizontal overflow.
- [x] 3.6 Coordinate comments so 1440px and wider can show directory plus push panel, 1280px-1439px temporarily release the directory column for push comments, and compact or narrow widths retain the right-side drawer.
- [x] 3.7 Add focused component and Feed integration tests for truthful rows, profile navigation, collapse state preservation, directory-only rendering, scroll isolation, mutation coherence, comment presentation, and unchanged non-Following scenes.

## 4. Verification and Documentation

- [x] 4.1 Run targeted relation package and API-flow tests plus `go build ./cmd/feed ./cmd/worker`, resolving backend regressions.
- [x] 4.2 Run the Following directory, Feed, router, comments, and relation frontend tests plus `pnpm -C apps/web run build`, resolving strict TypeScript and production-build failures.
- [x] 4.3 Use Windows Chrome through Chrome DevTools to verify Following at 1440px, 1200px, 1024px, and 800px with directory open, collapsed, searched, paginated, profile navigation, directory wheel scrolling, and comments open or closed.
- [x] 4.4 Capture and review the required Following screenshots, confirm unsupported live or unread facts are absent, and verify no account-specific source data is committed as visual evidence.
- [x] 4.5 Update `docs/uiux.md`, `docs/modules/feed.md`, and issue 38 in `docs/当前问题.md` only after the browser matrix passes.
- [x] 4.6 Run `openspec validate --all --strict` and resolve all change-artifact or delta-spec errors.
