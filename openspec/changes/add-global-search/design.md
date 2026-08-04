## Context

`TopNav` renders an uncontrolled search input and a visual “搜索” label, but neither submits navigation nor calls an API. Public videos can only be discovered through Feed scenes and users can only be opened when another surface already exposes their ID. The only keyword query in the backend is owner-scoped creator management, so it cannot serve public discovery.

The first release must search videos and users, preserve the hand-written typed router, avoid a new search service, and follow GCFeed's domain/application/infrastructure/HTTP boundaries.

## Goals / Non-Goals

**Goals:**

- Provide public, stable, paginated video and user search.
- Make the shell search field functional through a typed, shareable `/search` destination.
- Enforce public visibility, account status, bounded input, cursor integrity, and stale-request isolation.
- Keep implementation replaceable by a stronger search index later.

**Non-Goals:**

- Searching comments, messages, private videos, collections, or recommendation explanations.
- Typo correction, semantic/vector search, autocomplete, trending suggestions, or saved recent searches.
- Adding Elasticsearch, Meilisearch, or another external service in the first version.
- Opening video search results as a continuous result queue.

## Decisions

### Add an application-owned search aggregation module

A new `search` application service will depend on narrow `VideoSearchIndex` and `UserSearchIndex` interfaces. PostgreSQL implementations remain in their owning persistence areas or in search-specific adapters, while the HTTP search handler only parses input and maps results.

This avoids putting cross-domain query logic in a handler and leaves room to replace either index independently.

### Use separate video and user endpoints

The API will expose:

- `GET /api/search/videos?q=...&cursor=...&limit=...`
- `GET /api/search/users?q=...&cursor=...&limit=...`

Separate endpoints keep result types and cursors explicit and allow the Web tabs to paginate independently. A mixed “comprehensive” response was considered but rejected because one result category could starve the other and cursor semantics would be unclear.

### Normalize, escape, rank, and bind cursors to the query

The service trims the query, validates 1-64 Unicode code points, and rejects invalid limits outside 1-50. Persistence uses parameterized `ILIKE` predicates with escaped `\`, `%`, and `_`.

Video ordering uses deterministic relevance buckets: exact title, title prefix, title contains, then description contains; ties use `published_at DESC, id DESC`. User ordering uses exact account, account prefix, nickname prefix, account contains, then nickname contains; ties use `updated_at DESC, id DESC`.

Opaque versioned cursors encode the normalized query plus the relevance and tie-break tuple. Reusing a cursor with a different query or result type returns an invalid-cursor error rather than mixing result sets.

### Enforce discovery visibility in the query

Video search returns only `published + public + media-ready` cards. User search returns only active accounts and maps only public profile fields. Search remains anonymously readable; a valid viewer may receive additive viewer action state for video cards if the existing hydration boundary supports it, but authentication never broadens visibility.

### Make `/search` query parameters the Web source of truth

The typed route union gains `/search`; `q` and `tab=videos|users` are validated from `window.location.search`. `TopNav` becomes a real form that navigates to the normalized URL on Enter or button activation. `SearchPage` keeps independent result state per tab and invalidates stale requests when the normalized query changes.

Empty input stays on the search page with a prompt and does not issue a broad API request.

## Risks / Trade-offs

- [Contains matching can become expensive as tables grow] → Keep input and page sizes bounded, ensure visibility predicates are selective, record query latency, and leave the index interfaces replaceable by trigram or external search later.
- [Relevance expressions and cursors can diverge] → Centralize ranking constants/expressions and test page boundaries with tied relevance and timestamps.
- [User-controlled wildcard characters can broaden matching] → Escape all `ILIKE` metacharacters and bind values as parameters.
- [TopNav input can drift from browser history] → Synchronize it from validated route search state on navigation and popstate changes.

## Migration Plan

1. Add the search domain/application contracts, PostgreSQL queries, handler, routes, and API tests.
2. Add typed frontend API functions, route parsing, search page, and top-nav form behavior.
3. Add responsive and accessibility coverage.

No data migration or new table is required. Rollback removes the routes and returns the top-nav field to a non-submitting state.

## Open Questions

None.
