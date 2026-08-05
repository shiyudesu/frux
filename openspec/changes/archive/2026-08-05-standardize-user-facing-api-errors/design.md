## Context

Frux handlers currently emit concise JSON errors through module-local branches such as `{"error":"invalid credentials"}`, `{"error":"invalid request"}`, and `{"error":"internal server error"}`. Many validation branches also serialize `err.Error()`. The Web `apiRequest<T>` client converts these values into `ApiError.message`, and most pages and hooks call `apiErrorMessage`, which currently returns any non-empty `Error.message`. Two component paths bypass even that helper and render `error.message` directly.

This creates three coupled problems:

1. Users see English protocol, infrastructure, or browser text.
2. The frontend cannot reliably distinguish invalid login credentials from an expired access token, or one conflict/not-found condition from another, without matching mutable text.
3. Fixes are implemented per page, as shown by search having safe 5xx and network handling while other flows do not.

The change spans all HTTP modules and most Web data surfaces. It must preserve existing status codes, authentication redirects, API compatibility, strict TypeScript, and the current actionable Chinese search behavior.

## Goals / Non-Goals

**Goals:**

- Establish stable machine-readable error codes for all first-party JSON API errors.
- Keep the legacy `error` field so existing clients and tests can migrate without a breaking response removal.
- Prevent raw backend, framework, browser, and arbitrary JavaScript error text from reaching user-visible UI.
- Provide one typed frontend resolver for known codes, network failures, unknown 4xx responses, unknown 5xx responses, and intentional local validation messages.
- Preserve security properties: invalid account and incorrect password remain indistinguishable, and internal failures reveal no infrastructure details.
- Migrate every current user-visible API error surface, not only the login page.

**Non-Goals:**

- Introducing multilingual locale selection or a third-party internationalization framework.
- Changing domain error values solely to make them suitable for display.
- Changing successful API payloads, HTTP status semantics, or session-expiry navigation.
- Localizing operational logs, metrics labels, worker errors, media processing diagnostics, or developer-only playback errors that are not API messages.
- Returning field-level validation metadata in this change.

## Decisions

### 1. Add an error code without removing the legacy error field

Non-success JSON responses will use this compatible shape:

```json
{
  "code": "AUTH_INVALID_CREDENTIALS",
  "error": "invalid credentials"
}
```

`code` is the stable client contract. `error` remains the existing concise compatibility value and is not considered safe UI content.

Error codes use uppercase snake case and describe product/API semantics rather than Go package names. Shared categories include invalid request, invalid access token, forbidden, not found, conflict, rate limited, service unavailable, and internal error. Domain-specific codes are used where the UI benefits from a distinct action, such as invalid credentials, account already exists, private liked videos, comment permission denied, or an invalid search cursor.

**Alternative considered:** Translate existing error strings in the frontend. Rejected because text matching is brittle, dynamic `err.Error()` values are numerous, and refactors could silently break localization.

**Alternative considered:** Return Chinese messages directly from every handler. Rejected because it couples the API contract to one presentation language and still leaves browser/network errors inconsistent.

### 2. Centralize backend envelope writing but retain module-owned status mapping

A shared Interfaces-layer helper will write the JSON envelope and define reusable code constants for common protocol errors. Each module's existing `write{Module}Error` function remains responsible for `errors.Is` checks and HTTP status selection because those mappings belong to the module boundary.

Repeated branches such as invalid JSON, missing/invalid access token, and unexpected internal failure will use shared helpers or constants. Domain-specific branches will supply their stable code and legacy error value explicitly.

This keeps dependency direction unchanged: Domain and Application packages do not import HTTP error types.

**Alternative considered:** Create a global middleware that converts all returned errors. Rejected because Hertz handlers currently write responses directly and module-specific domain mapping would either be lost or require a much larger handler signature redesign.

### 3. Make raw server text diagnostic-only in the typed Web client

`ApiErrorBody` gains an optional `code`. `ApiError` preserves:

- `status`
- optional `code`
- the legacy backend text for diagnostics

User-visible code will not read `ApiError.message` or the raw response fields directly. The client remains tolerant of older responses without `code`.

Upload paths using `XMLHttpRequest` will construct the same typed `ApiError` shape as `fetch`, so direct and multipart uploads share the same resolver behavior.

### 4. Resolve messages with explicit trust boundaries

The shared resolver applies this priority:

1. An explicitly user-facing local validation error returns its authored Chinese message.
2. A transport/network failure returns `网络连接失败，请检查网络后重试`.
3. A recognized API error code returns its centralized Chinese mapping.
4. An unknown `429` returns a rate-limit message.
5. An unknown `5xx` returns an action-appropriate temporary-failure fallback.
6. Any other unknown API error returns the caller's action-appropriate fallback.
7. Arbitrary unknown JavaScript errors return the fallback.

Frontend-authored validation messages will use a dedicated error type or direct validation result rather than ordinary `Error`, making the decision to expose text explicit.

The resolver may continue accepting a caller fallback so pages retain useful context such as `作品加载失败`, `评论发布失败`, or `合集保存失败`. It will normalize temporary-failure suffixes to avoid duplicated wording.

**Alternative considered:** Display raw text when it contains Chinese characters. Rejected because language detection is not a trust boundary and could expose unintended server details.

### 5. Keep authentication control flow separate from display text

`AUTH_INVALID_CREDENTIALS` is used only by password login and maps to `账号或密码错误，请重新输入`. `AUTH_INVALID_ACCESS_TOKEN` identifies missing, malformed, or expired authenticated sessions and continues to drive `isUnauthorized`, session clearing, and navigation to `/auth`.

`isUnauthorized` may continue using status `401` for compatibility, but code-aware checks will prevent login credential failures from being confused with an expired established session when a caller needs the distinction.

### 6. Migrate all visible call sites and preserve search behavior

All current `apiErrorMessage` usages and the direct `error.message` paths in action controls will use the safe resolver. Search-specific error logic will be reduced to code mappings and action fallbacks without regressing its existing Chinese validation, network, and unavailable-service messages.

Errors used only for programming invariants, player adapter diagnostics, tests, or developer telemetry remain outside this migration unless they cross a user-visible boundary.

### 7. Verify the contract at backend, client, and component levels

Backend tests will assert status, stable code, compatible legacy error, and internal-detail redaction for representative common and module-specific mappings. A focused inventory test or equivalent coverage will ensure every first-party JSON error response uses the shared envelope.

Frontend tests will cover the resolver matrix, missing-code compatibility, multipart upload errors, and login/register component behavior. Authentication tests will verify unknown account and incorrect password show the same Chinese message. Existing search tests will be retained or migrated to the shared resolver.

## Risks / Trade-offs

- **[Risk] A handler branch is missed during migration** → Use a repository-wide inventory of JSON error writes, migrate all modules, and add targeted contract coverage for shared/common branches.
- **[Risk] Backend codes and frontend mappings drift** → Define backend constants, centralize the frontend catalog, and test representative codes used by every affected module.
- **[Risk] Additive `code` fields break exact-body tests** → Update tests to decode JSON or assert the expanded compatible envelope rather than retaining obsolete exact strings.
- **[Risk] Generic fallbacks reduce useful detail for an unmapped business error** → Assign specific codes to actionable business cases and use caller-specific fallbacks for unknown conditions.
- **[Risk] A developer reintroduces direct `error.message` rendering** → Remove current API-facing occurrences, document the trust boundary, and add focused tests around shared display helpers and affected components.
- **[Trade-off] The legacy `error` field continues carrying English text** → This preserves API compatibility; the Web treats it as diagnostic-only and a later versioned API can remove it separately.

## Migration Plan

1. Add shared backend error envelope helpers and common stable error-code constants.
2. Migrate every first-party JSON error branch module by module while retaining current HTTP status and legacy `error` value.
3. Extend `ApiErrorBody`, `ApiError`, `apiRequest`, and upload transport errors to preserve optional codes.
4. Add the safe resolver, centralized Chinese code catalog, and explicit user-facing local validation mechanism.
5. Replace all user-visible raw error paths, preserving existing unauthorized redirects and search messages.
6. Add backend and frontend tests, then update engineering/module documentation and mark issue 11 resolved.

Rollback is safe because the backend change is additive and the frontend accepts responses with or without `code`. If frontend deployment must be reverted, the retained legacy `error` field continues serving the previous client.

## Open Questions

None. Field-level validation details and full multilingual localization are intentionally deferred.
