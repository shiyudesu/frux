## 1. Baseline and Runtime

- [x] 1.1 Run the existing backend build and Go test suite, inventory the 39 registered routes, and record any pre-existing failures before changing the HTTP framework.
- [x] 1.2 Add a current compatible Hertz dependency, replace `internal/infra/httpgin` with a Hertz runtime package, and configure host ports, recovery/logging, the 1 GiB body ceiling, streaming behavior, and large-buffer retention.
- [x] 1.3 Update `cmd/feed/main.go` to initialize, register, and start the Hertz server while preserving configuration loading, database initialization, startup logging, and fatal error handling.

## 2. Routing and HTTP Integrations

- [x] 2.1 Convert `router.Register` and every public and internal route group to Hertz while preserving all methods, paths, middleware ordering, and dependency assembly.
- [x] 2.2 Convert the health handler and register `promhttp.Handler` through the official Hertz `http.Handler` adaptor.
- [x] 2.3 Serve `/uploads` GET requests through an explicitly prefix-stripped standard file server adapted to Hertz and preserve HEAD metadata with a native Hertz handler.
- [x] 2.4 Configure Hertz router defaults or explicit handlers where needed to preserve current trailing-slash, method-not-allowed, and not-found behavior.

## 3. Middleware and Request Context

- [x] 3.1 Convert required JWT, optional JWT, and internal-token middleware to Hertz handler signatures and preserve abort responses, constant-time token comparison, and request-scoped identity keys.
- [x] 3.2 Convert the HTTP Prometheus middleware to read Hertz method, normalized route, and final status while preserving collector names and stable label cardinality.
- [x] 3.3 Update authenticated-user and role lookup helpers to read Hertz request-context keys without allowing pooled request contexts to escape the request lifecycle.

## 4. JSON and Resource Handlers

- [x] 4.1 Migrate account and video handlers to Hertz signatures, `BindJSON`, explicit path/query/header access, standard context propagation, and compatible JSON/error responses.
- [x] 4.2 Migrate feed, recommendation, exposure, and playback handlers while preserving optional identity, cursor/query behavior, internal request DTOs, and status mappings.
- [x] 4.3 Migrate interaction, relation, and message handlers while preserving authentication, idempotency headers, role checks, pagination, and state-change responses.
- [x] 4.4 Replace all remaining Gin response maps, handler types, request access, and helper signatures under `internal/interfaces/http` with Hertz-compatible equivalents.

## 5. Multipart Upload Handling

- [x] 5.1 Migrate the upload handler to Hertz multipart APIs without buffering an entire maximum-sized video in memory.
- [x] 5.2 Preserve the total, video, image, and generic-file size limits plus extension, MIME, codec, duration, dimension, filename, and target-directory rules.
- [x] 5.3 Preserve request cancellation for ffprobe/ffmpeg work and cleanup of temporary or target files after validation and faststart failures.
- [x] 5.4 Add or update upload tests for valid files, invalid kinds, unsupported content, per-kind oversize rejection, server-boundary body limits, processing failures, and persisted-file cleanup.

## 6. Hertz API Test Harness

- [x] 6.1 Replace Gin test server and `net/http/httptest` route helpers with shared Hertz `pkg/common/ut.PerformRequest` helpers for JSON bodies, headers, multipart requests, status assertions, and response decoding.
- [x] 6.2 Migrate account, video, feed, interaction, and relation API-flow tests to the Hertz route tree without reducing authentication, idempotency, pagination, or state assertions.
- [x] 6.3 Migrate message, recommendation, exposure, playback, and upload API-flow tests to the Hertz route tree without reducing success and failure coverage.
- [x] 6.4 Migrate the static upload range regression test and assert GET, HEAD, `206 Partial Content`, `Content-Range`, and selected response bytes.
- [x] 6.5 Add focused tests for Hertz middleware context propagation, unauthorized abort behavior, health output, metrics route access, and normalized metric route labels.

## 7. Dependency and Documentation Cleanup

- [x] 7.1 Remove Gin from `go.mod` and `go.sum`, rename Gin-specific packages/import aliases/comments, run dependency cleanup, and verify repository searches contain no production or test Gin references.
- [x] 7.2 Update `README.md`, `docs/engineering.md`, `docs/architecture.md`, `docs/quickread.md`, `docs/optimization.md`, `docs/interview-questions.md`, `.github/copilot-instructions.md`, and `openspec/project.md` from Gin to Hertz.
- [x] 7.3 Review Compose, nginx, Vite proxy, health-check, metrics, and startup-script configuration to confirm that unchanged ports and paths require no deployment edits.

## 8. Verification

- [x] 8.1 Run targeted authentication, upload, static range, metrics, and representative API-flow tests and fix all compatibility regressions.
- [x] 8.2 Run `go build ./cmd/feed ./cmd/worker` and `go test ./...` from `apps/api`.
- [x] 8.3 Run `docker compose config` from `apps` and `openspec validate --all --strict` from the repository root.
- [x] 8.4 Compare the final route inventory and public/internal contract behavior with the baseline, confirming that only the HTTP framework and related implementation details changed.
