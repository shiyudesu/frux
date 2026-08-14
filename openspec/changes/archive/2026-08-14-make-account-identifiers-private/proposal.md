## Why

Frux currently uses the login `account` both as a credential identifier and as a public identity across profiles, search, relationship lists, comments, and browser caches. Account identifiers should be private to their owner so another user cannot view or use them to confirm that an account exists.

## What Changes

- **BREAKING** Redefine `account` as a private login identifier that is returned only through owner-authenticated, credential, privileged administration, or internal service boundaries.
- **BREAKING** Remove account identifiers from public profile, user search, following/follower list, and comment identity responses.
- Change user search and Following-directory filtering to match nicknames only; exact account lookup is no longer a public discovery mechanism.
- Keep the authenticated owner profile and account-security experience able to display the current user's own account.
- Stop rendering or caching another user's account in the Web client and sanitize legacy public-profile cache entries.
- Preserve public navigation through numeric user IDs and public presentation through nickname, avatar, bio, and existing public statistics.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `account-identity`: Define the account identifier as private credential data and constrain which boundaries may expose it.
- `profile-dashboard`: Remove account identifiers from visitor-facing profile responses and presentation while retaining owner visibility.
- `global-search`: Search users by nickname only and omit account identifiers from user results.
- `following-feed-directory`: Search followed users by nickname only and omit account identifiers from relationship rows.
- `threaded-comments`: Project comment authors and reply targets using public identity fields without account identifiers.

## Impact

- Affected backend modules: account HTTP responses, account search persistence, relation list persistence and HTTP responses, interaction comment projections, and search/relation cursor compatibility.
- Affected Web surfaces: public profiles, user search, Following directory, relationship types, comment types, profile navigation helpers, and public-profile local storage.
- Public JSON response shapes and account-based user searches change incompatibly; deployment must account for stale Web clients and invalidated query-bound cursors.
- The `account` database column, registration and login behavior, authenticated `/api/users/me` response, and privileged administration remain intact; no database schema migration is required.
- OpenSpec and account, search, relation, and interaction documentation must be synchronized with the new privacy boundary.
