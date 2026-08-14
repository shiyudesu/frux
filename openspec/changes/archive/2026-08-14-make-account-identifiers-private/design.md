## Context

The canonical `account` value is stored on the account aggregate and is required for registration, login lookup, uniqueness, and privileged account administration. It is also currently copied into public profile, search, relationship, and comment response models, rendered by several Web surfaces, and persisted in the public-profile `localStorage` cache.

This coupling makes a credential identifier part of the public identity model. It also conflicts with the profile-dashboard requirement that visitor-facing profiles omit private account data. The change spans account, search, relation, interaction, and Web boundaries, but it does not require a database migration because the credential remains stored and normalized as before.

## Goals / Non-Goals

**Goals:**

- Make `account` private to the account owner and privileged or internal account operations.
- Remove non-empty account identifiers from every cross-user response and Web presentation.
- Prevent public user discovery and relationship filtering by account identifier.
- Preserve public navigation and identity presentation using numeric user ID, nickname, avatar, bio, and existing public statistics.
- Remove account identifiers from new and legacy public-profile browser cache entries.
- Change query-bound cursor versions safely when search semantics change.

**Non-Goals:**

- Renaming or removing the persisted `account` column.
- Changing account normalization, uniqueness, registration, login, password management, or authenticated owner-profile behavior.
- Hiding account identifiers from explicitly privileged administration or trusted internal service boundaries.
- Introducing a new globally unique public handle.
- Attempting to revoke knowledge already observed or stored outside Frux before this change.

## Decisions

### 1. Treat account and public identity as separate concepts

The login account remains on the account aggregate, authenticated owner profile, and privileged account administration. Cross-user projections use only:

```text
user_id + nickname + avatar_url + optional bio/public statistics
```

Public response DTOs SHALL omit account fields rather than masking or hashing them. Masked identifiers still leak structure and stable correlation, while empty permanent fields preserve a misleading public contract.

### 2. Enforce privacy at response and query boundaries

The backend removes account identifiers before they cross a user-facing boundary. Hiding text only in React is insufficient because browser network tools, API clients, and caches would still receive the value.

- Public profile mapping omits `account`.
- User search persistence neither selects nor matches `account`.
- Following and follower list persistence neither selects nor matches `account`.
- Comment author and direct-reply projections omit account fields from domain presentation models and HTTP DTOs.
- Private owner and privileged administration responses remain unchanged.

### 3. Make nickname the only public user-search key

Public user search ranks case-insensitive nickname matches as exact, prefix, then contains, followed by `updated_at DESC, id DESC`. Following-directory filtering uses case-insensitive nickname containment only.

Duplicate nicknames remain valid. Search results are disambiguated by avatar, bio, and destination user ID. If Frux later needs a unique shareable name, it should add a separate `public_handle` with its own lifecycle rather than expose the login account again.

### 4. Keep numeric user IDs as navigation identity

Existing `/users/{userId}` routes, follow targets, comment identities, and cache keys already use numeric IDs. Removing the account field therefore does not require route migration or relationship-key changes.

Nickname is presentation data and MUST NOT become an authorization key, persistence key, or route identifier.

### 5. Separate owner and visitor profile presentation

The shared profile hero may continue to display the account row for `owner=true`, but visitor-facing rendering must not accept or fall back to another user's account. Public display-name fallbacks use the nickname and then a neutral `用户_{id}` label.

Gender remains an independently public profile field and must not disappear merely because its current visual placement shares the account row.

### 6. Sanitize public-profile browser storage

`StoredPublicProfile` and `PublicProfileInput` no longer contain `account`. Comment and reply navigation helpers stop seeding account values. Reading the existing `PUBLIC_PROFILE_KEY` cache projects every valid entry into the new account-free shape and rewrites or removes legacy account-bearing entries so refreshed clients do not continue retaining the identifier.

The migration cannot make users forget values already seen, but it prevents continued Frux rendering and storage after upgrade.

### 7. Version cursors whose matching semantics change

User-search cursors use a user-specific version newer than the current version while video-search cursors retain their existing version. Old user cursors are rejected because their relevance values and result set may depend on account matching.

Relationship list cursors generated for nickname filtering use a new version. Existing versioned cursors with a non-empty query are rejected after the change. Unfiltered legacy cursors may remain accepted because their ordering tuple is unchanged.

### 8. Coordinate the breaking response change

The Web client must stop requiring, rendering, caching, or locally matching account fields before or atomically with the backend response cutover. If Web and API cannot be deployed atomically, deploy the tolerant Web first, then remove backend fields.

Permanent compatibility fields are rejected because they would keep the private identifier in the public contract. A short operational redaction window using empty values is acceptable only if needed to protect already-loaded legacy Web clients; the final contract omits the fields.

## Risks / Trade-offs

- **[Breaking API consumers]** Removing JSON fields can break stale or external clients. → Deploy the tolerant Web first or atomically, document the contract change, and test missing-field behavior.
- **[Existing search links or cursors fail]** Account-only searches return no users and old query cursors become invalid. → Return the existing validation error for incompatible cursors and let the Web restart pagination.
- **[Duplicate nickname ambiguity]** Users with the same nickname are harder to distinguish. → Keep avatar, bio, and numeric profile destination available; design a separate public handle later if needed.
- **[Legacy browser storage retains identifiers]** Previously cached account values may survive a simple type change. → Explicitly sanitize and rewrite the public-profile cache.
- **[Privacy regression through a new DTO]** A future cross-user projection could copy `account` again. → Add negative API assertions for account fields and document the private boundary in the account identity specification.
- **[Rollback re-exposes data]** Reverting the backend response mapping would violate the new privacy guarantee. → Prefer forward fixes; any rollback must preserve account redaction even if unrelated UI or search changes are reverted.

## Migration Plan

1. Update Web types and components to operate without cross-user account fields, remove account-based matching and display, and sanitize public-profile storage.
2. Update backend public DTOs and projections to omit account identifiers.
3. Change user and Following-directory queries to nickname-only matching and introduce the new cursor versions.
4. Update focused API, persistence, Web component, hook, cache-migration, and negative privacy tests.
5. Synchronize OpenSpec and account, search, relation, and interaction documentation.
6. Deploy Web before the API contract cutover when independent deployment is required; otherwise release them atomically.
7. Monitor public response samples and search validation errors without logging account query values.

Rollback must keep public account values redacted. Database rollback is unnecessary because no schema or stored account data changes.

## Open Questions

None. The selected product boundary is that the account is a private login identifier and is not publicly searchable.
