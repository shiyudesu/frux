## 1. Backend Error Contract

- [x] 1.1 Add a shared Interfaces-layer JSON error envelope with stable uppercase error codes, the backward-compatible `error` field, and helpers for common invalid-request, invalid-access-token, unavailable, and internal failures.
- [x] 1.2 Add focused tests for the shared writer covering JSON shape, HTTP status preservation, legacy-field compatibility, and redaction of wrapped infrastructure details.
- [x] 1.3 Extend account error mapping with stable registration, validation, invalid-credentials, invalid-access-token, not-found, and internal codes while preserving identical unknown-account and incorrect-password responses.
- [x] 1.4 Add account API-flow assertions for duplicate registration, missing input, unknown account, incorrect password, invalid access token, and unexpected internal failure codes.

## 2. Backend Handler Migration

- [x] 2.1 Migrate feed, search, recommendation, exposure, and playback JSON error branches to the shared envelope without changing existing status semantics or actionable search behavior.
- [x] 2.2 Migrate interaction, relation, message, and library JSON error branches, assigning distinct codes to actionable permission, resource, validation, and idempotency failures.
- [x] 2.3 Migrate video and creator-content JSON error branches, including collection, visibility, media-asset, validation, permission, conflict, and internal failures.
- [x] 2.4 Migrate multipart and direct-upload session error branches so validation, authorization, ownership, conflict, unavailable-storage, and internal failures use the shared envelope.
- [x] 2.5 Update representative handler and API-flow tests in every migrated module to assert stable codes and verify internal errors do not expose infrastructure text.
- [x] 2.6 Perform a repository-wide inventory of first-party JSON error writes and remove or migrate every remaining handler branch that returns an error payload without a stable code.

## 3. Typed Web Error Boundary

- [x] 3.1 Extend `ApiErrorBody` and `ApiError` with an optional stable code and diagnostic-only legacy text while preserving status-based authentication control flow.
- [x] 3.2 Implement the centralized Chinese error-code catalog, safe resolver, explicit user-facing local validation type, network fallback, rate-limit fallback, unknown 4xx fallback, and temporary 5xx fallback.
- [x] 3.3 Add resolver unit tests covering known codes, missing-code legacy responses, arbitrary English and Chinese raw messages, network failures, unknown JavaScript errors, local validation errors, `429`, and `5xx`.
- [x] 3.4 Update fetch and both upload transports to create the same typed API/transport errors and never treat response or browser text as safe display content.
- [x] 3.5 Add upload client tests for coded API failures, legacy payloads, malformed responses, and network failures.

## 4. Authentication and User-Visible Surfaces

- [x] 4.1 Update the login/register page to use the shared resolver, show `账号或密码错误，请重新输入` for invalid credentials, and show a distinct friendly duplicate-account message.
- [x] 4.2 Add login/register component tests for unknown accounts, incorrect passwords, duplicate accounts, validation failures, network failures, and internal failures.
- [x] 4.3 Migrate feed loading, recommendation feedback, watch-later controls, comments, likes, favorites, and follows away from direct `ApiError.message` or `Error.message` rendering while preserving optimistic rollback and unauthorized navigation.
- [x] 4.4 Migrate messages, profile editing, relation lists, public profiles, creator content, collections, and personal-library error states to the shared resolver.
- [x] 4.5 Migrate upload-page local validation and API failures to explicit user-facing validation errors and the shared resolver.
- [x] 4.6 Replace search-specific raw-message handling with shared error codes and fallbacks while retaining its existing Chinese validation, network, and unavailable-service messages and tests.
- [x] 4.7 Audit all Web pages, hooks, and components to ensure API, transport, browser, and arbitrary JavaScript error text is never directly rendered to users.

## 5. Documentation and Validation

- [x] 5.1 Document the additive `{code, error}` API contract, code ownership, frontend trust boundary, and fallback policy in `docs/engineering.md`.
- [x] 5.2 Update account and other module documents that enumerate error responses, including the indistinguishable credential-failure behavior and stable codes.
- [x] 5.3 Run targeted backend handler/API-flow tests, then `go test ./...` from `apps/api`.
- [x] 5.4 Run the frontend Vitest suite and `pnpm -C apps/web run build`.
- [x] 5.5 Run `openspec validate --all --strict`, verify the implemented behavior against both delta specs, and mark item 11 in `docs/当前问题.md` resolved only after all checks pass.
