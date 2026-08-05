## Why

Frux currently forwards many backend and browser error strings directly into user-visible UI, causing messages such as `invalid credentials`, `internal server error`, and `Failed to fetch` to appear across authentication, comments, relations, messages, creator tools, libraries, and uploads. The login issue in `docs/当前问题.md` exposes a broader contract gap: API errors lack stable machine-readable codes, while the Web client has no safe, consistent localization and fallback policy.

## What Changes

- Add a stable, machine-readable error code to API error responses while retaining the existing `error` field for compatibility.
- Define consistent API error categories for validation, authentication, authorization, missing resources, conflicts, rate limiting, unavailable dependencies, and unexpected internal failures.
- Extend the typed Web API client to preserve HTTP status and error code without treating backend text as safe display content.
- Add centralized Chinese user-message resolution for known error codes, network failures, and unknown 4xx/5xx responses.
- Ensure invalid login accounts and incorrect passwords produce the same user-friendly message without revealing whether an account exists.
- Replace direct rendering of `ApiError.message` and equivalent backend/browser error text across all user-visible Web flows.
- Preserve existing actionable Chinese search errors while moving them onto the shared error-handling path.
- Add backend contract tests and frontend unit/component tests covering login, registration, network failures, known business errors, unknown client errors, and internal failures.
- Update engineering and account documentation, then mark issue 11 as resolved after implementation is verified.

## Capabilities

### New Capabilities

- `user-facing-api-errors`: Defines stable API error codes, backward-compatible error envelopes, safe frontend localization, and user-friendly fallback behavior across all Web surfaces.

### Modified Capabilities

- `web-frontend`: Strengthens the typed API boundary so user-visible components consume centralized safe error messages rather than raw backend or browser error strings.

## Impact

- Backend HTTP handlers under `apps/api/internal/interfaces/http` and shared response helpers.
- API flow and handler tests that assert error payloads.
- Frontend API types and client logic under `apps/web/src/api/client.ts` and `apps/web/src/types.ts`.
- Authentication, feed, comments, messages, profile, public profile, creator content, personal libraries, recommendation feedback, and upload error surfaces.
- Frontend Vitest coverage for centralized error resolution and authentication behavior.
- `docs/engineering.md`, relevant module documentation, and `docs/当前问题.md`.
- Existing clients remain compatible because HTTP statuses and the legacy `error` string field are retained; the new `code` field is additive.
