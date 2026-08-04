## 1. Search Contracts and Application Logic

- [x] 1.1 Create the search domain/application contracts, result types, validation errors, relevance tuples, and versioned query-bound cursor codecs.
- [x] 1.2 Implement video and user search service methods with independent pagination and public result mapping.
- [x] 1.3 Add unit tests for Unicode query limits, wildcard escaping, cursor/query/category binding, relevance ties, and page boundaries.

## 2. PostgreSQL Search Indexes

- [x] 2.1 Implement parameterized public video search over title/description with published, public, and media-ready filtering.
- [x] 2.2 Implement parameterized active user search over account/nickname with public field projection.
- [x] 2.3 Add persistence tests covering exact, prefix, contains, case-insensitive, wildcard-literal, unavailable-video, and inactive-user behavior.

## 3. HTTP API and Composition

- [x] 3.1 Add typed video/user search DTOs and handlers for `GET /api/search/videos` and `GET /api/search/users`.
- [x] 3.2 Wire search repositories, service, handlers, and routes through the main composition root with consistent validation/error mapping.
- [x] 3.3 Add API-flow tests for anonymous access, validation, pagination, cursor misuse, visibility, and result navigation identifiers.

## 4. Typed Web Search Experience

- [x] 4.1 Add search request/response types and typed API functions for video and user result pages.
- [x] 4.2 Extend the hand-written router with `/search` plus validated `q` and `tab` parsing and dispatch `SearchPage` from `App.tsx`.
- [x] 4.3 Implement `SearchPage` with independent video/user state, generation guards, prompt/loading/error/empty/ready/loading-more states, and existing video/profile navigation.
- [x] 4.4 Convert `TopNav` search into a controlled accessible form synchronized with browser history and route state.
- [x] 4.5 Add desktop and mobile search-result styling without horizontal overflow or interference with Feed keyboard shortcuts.

## 5. Tests, Documentation, and Validation

- [x] 5.1 Add router, top-navigation, search-state, stale-response, tab-retention, and result-navigation frontend tests.
- [x] 5.2 Add `docs/modules/search.md` and update engineering, API, UI/UX, and current-issue documentation for the new capability.
- [x] 5.3 Run targeted search backend/frontend tests, the Go build, the frontend production/type-check build, and strict OpenSpec validation.
