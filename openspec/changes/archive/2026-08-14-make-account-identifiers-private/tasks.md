## 1. Make the Web Tolerant of Private Accounts

- [x] 1.1 Remove cross-user `account`, `user_account`, and `reply_to_user_account` fields from public profile, search, relation, comment, and public-cache TypeScript contracts while retaining `UserProfile.account` for the authenticated owner.
- [x] 1.2 Update the shared profile hero and public profile page so only the owner view renders the account identifier, public display-name fallbacks use nickname or neutral user ID text, and public gender remains visible independently.
- [x] 1.3 Update user search and Following-directory presentation, copy, fallback labels, and local filtering to use nickname, avatar, and bio without account text or account matching.
- [x] 1.4 Remove comment and reply account values from public-profile navigation helpers and ensure comment identity navigation continues to use numeric user IDs.
- [x] 1.5 Sanitize `PUBLIC_PROFILE_KEY` reads and writes so legacy account-bearing entries are projected and persisted in the new account-free shape.

## 2. Remove Account Identifiers from Backend Public Projections

- [x] 2.1 Remove `account` from the public account profile DTO and response mapper while preserving it in registration and authenticated `/api/users/me` responses.
- [x] 2.2 Change search domain, application, persistence, and HTTP user-result models to return only ID, nickname, avatar, and bio and to query nickname without selecting or matching account.
- [x] 2.3 Change relation domain, persistence, application, and HTTP list models to omit account and make Following-directory search match nickname only; keep unfiltered following and follower ordering unchanged.
- [x] 2.4 Remove comment author and direct-reply account fields from interaction domain projections, threaded comment queries, HTTP DTOs, and response mapping while preserving user IDs, nicknames, avatars, tombstones, and authorization behavior.

## 3. Preserve Pagination Safety

- [x] 3.1 Replace account-based user-search relevance with exact, prefix, and contains nickname relevance and introduce a user-specific cursor version that rejects pre-privacy user cursors without invalidating video-search cursors.
- [x] 3.2 Advance relationship query cursor compatibility so pre-privacy non-empty search cursors are rejected while compatible unfiltered legacy cursors may continue with the unchanged ordering tuple.

## 4. Verify Privacy and Existing Behavior

- [x] 4.1 Add account API tests that assert public profile JSON has no `account` field while owner and registration responses still expose the canonical account.
- [x] 4.2 Update search API, application, and PostgreSQL query tests for nickname-only relevance, account-only non-matches, omitted account fields, wildcard escaping, stable pagination, and old user-cursor rejection.
- [x] 4.3 Update relation API, persistence, hook, and Following-directory tests for nickname-only filtering, omitted account fields, compatible unfiltered pagination, and rejected pre-privacy query cursors.
- [x] 4.4 Update interaction API, persistence, hook, and threaded-comment tests to assert account fields are absent while identity navigation, reply targets, tombstones, permissions, and author markers remain correct.
- [x] 4.5 Add Web tests proving public profiles, search results, relationship rows, and comment-seeded profile caches never render or retain another user's account, including migration of a legacy local-storage entry.
- [x] 4.6 Run targeted Go tests for account, search, relation, and interaction packages and API flows, then run the frontend test selectors and `pnpm -C apps/web run build`.

## 5. Synchronize Documentation and Validate the Change

- [x] 5.1 Update `docs/modules/account.md`, `search.md`, `relation.md`, and `interaction.md` so account is documented as a private login identifier and all public examples use nickname-based identity.
- [x] 5.2 Review other product and UI documentation for claims that account is a public handle and replace them with the private-account boundary or a future separate public-handle note.
- [x] 5.3 Run `openspec validate --all --strict` and resolve every artifact or specification validation error.
