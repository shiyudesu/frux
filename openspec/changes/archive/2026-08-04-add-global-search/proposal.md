## Why

The top navigation displays a search field that has no behavior, and the backend has no public discovery query for videos or users. Users cannot search the catalog or navigate directly to creators from the shell.

## What Changes

- Add global search for readable public videos and active public users.
- Add stable, independently paginated video and user search results with trimmed, validated query input.
- Add a typed `/search` route whose query string is the shareable source of truth for the search term and active result category.
- Wire the top navigation search form to submit through the existing hand-written router.
- Open video results through the existing video destination and user results through public profiles.
- Provide loading, error, empty, pagination, keyboard, mobile, and stale-request states without displaying unsupported result types.

## Capabilities

### New Capabilities

- `global-search`: Public video and user search APIs plus the typed Web search experience.

### Modified Capabilities

- `web-frontend`: Add typed search routing and API boundaries without introducing a routing library.
- `douyin-style-web-experience`: Make the existing shell search control functional and responsive.

## Impact

- Adds an application-level search module with narrow video and account search interfaces, PostgreSQL query implementations, HTTP handlers, router wiring, tests, and module documentation.
- Affects `TopNav`, `router.tsx`, `App.tsx`, frontend API/types, a new search page, and responsive styles.
- No new external search service or dependency is required for the first version.
