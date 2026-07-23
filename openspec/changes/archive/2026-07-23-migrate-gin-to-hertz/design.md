## Context

The API process currently starts a Gin engine, registers 39 public, internal, health, and metrics routes, and exposes local uploads from the same process. Gin types appear only at the HTTP boundary: startup, routing, handlers, middleware, HTTP metrics, upload handling, and 11 API test files. Domain, application, persistence, cache, messaging, JWT, worker, and frontend code are framework-independent and must remain so.

The official `hertz-contrib/migrate` Gin path is a legacy Python source-rewrite script. Its conversion table targets older Hertz APIs and does not handle several patterns used by GCFeed, including `ShouldBindJSON`, `c.Request.Context()`, anonymous middleware, `gin.WrapH`, upload body limits, startup, and `ServeHTTP` tests. It can accelerate mechanical edits, but it cannot define or prove the final migration.

Constraints:

- Existing public and internal HTTP contracts must remain compatible.
- Existing upload validation, cleanup, video processing, static URLs, and byte-range playback must remain intact.
- Prometheus metric names and stable route labels must not change.
- Gin must not leak into domain or application packages during transition.
- The migration must leave a single production HTTP framework rather than a permanent Gin compatibility layer.

## Goals / Non-Goals

**Goals:**

- Run the API process on a current Hertz release compatible with the repository's Go version.
- Preserve route paths, methods, request and response JSON, headers, status codes, authentication, idempotency, cursor behavior, health checks, metrics, uploads, and static byte-range responses.
- Keep application services dependent on standard `context.Context`, not Hertz request types.
- Replace Gin-specific tests with Hertz in-process route tests that retain the current API-flow coverage.
- Remove Gin and rename framework-specific infrastructure and documentation.
- Keep the migration reviewable and reversible until the complete Go test suite passes.

**Non-Goals:**

- Changing business requirements, endpoint versions, DTO schemas, persistence models, cache keys, RabbitMQ messages, JWT claims, or frontend behavior.
- Introducing Hertz IDL/code generation, Kitex, service discovery, HTTP/2, TLS termination, or new middleware capabilities.
- Benchmark-driven tuning beyond avoiding clear regressions such as buffering large uploads unnecessarily.
- Running Gin behind Hertz as a long-term compatibility mode.

## Decisions

### 1. Replace the HTTP boundary directly

The production API will use one Hertz server and one route tree. `internal/infra/httpgin` will be replaced by a Hertz-specific infrastructure package, and `router.Register` will accept the Hertz server type. Framework-level trailing-slash redirects will be disabled and recreated as explicit route variants so redirects pass through the same middleware and streamed-body cleanup as normal handlers.

**Rationale:** Gin coupling is localized to 14 production files, while the business and infrastructure layers are already isolated. A direct replacement is simpler than operating two routing stacks and avoids double routing, adapter overhead, and ambiguous middleware ownership.

**Alternative considered:** Mount the Gin engine behind `adaptor.HertzHandler` and migrate routes incrementally. This lowers initial cutover risk but retains both frameworks, complicates route precedence and metrics, and delays removal of Gin.

### 2. Treat the migration script as an optional draft generator

If used, `hertz-contrib/migrate` will be cloned at a reviewed commit and run only on a clean, disposable migration branch. Its output will be inspected and corrected before it is accepted. The pipe-to-shell command from the tutorial will not be part of the implementation workflow.

**Rationale:** The script performs broad source and dependency edits and cannot correctly migrate several GCFeed patterns. Pinning and reviewing it reduces supply-chain and nondeterministic-change risk.

**Alternative considered:** Perform every mechanical edit manually. This is safe but may add repetitive work; either approach must converge on the same reviewed implementation.

### 3. Use Hertz's two-context handler model deliberately

Handlers and middleware will use:

```text
func(context.Context, *app.RequestContext)
```

The standard `context.Context` argument will be passed to all application service calls. Hertz client-disconnection sensing and a five-minute request read timeout will bound abandoned or slow request work. Bounded JSON and multipart readers will mark streams they fully consume; any unconsumed stream remaining after its handler completes will close the connection and detach the stream so Hertz does not drain attacker-controlled unread bytes. Authentication identity and role will remain request-scoped values stored in `RequestContext.Keys`, because they are consumed synchronously by downstream middleware and handlers.

**Rationale:** This preserves cancellation and application interfaces while preventing pooled Hertz request contexts from escaping the request lifecycle.

**Alternative considered:** Pass `RequestContext` into application services. This would violate the current dependency direction and couple business use cases to Hertz.

### 4. Preserve DTO and error semantics with explicit Hertz APIs

JSON request bodies will use a shared 4 MiB bounded decoder over Hertz's buffered or streamed request body; query, path, form, and header values will continue to be parsed explicitly. Responses will use Hertz JSON/status helpers with the existing DTOs and error mappings. Binding failures will continue to return the existing concise `400` payloads instead of exposing framework error details.

The bounded decoder will use standard JSON unmarshalling to keep memory usage deterministic and DTO behavior consistent.

**Rationale:** Explicit source-specific binding avoids the precedence changes that can occur with generic multi-source binding and keeps handler responsibilities aligned with `docs/engineering.md`.

### 5. Adapt standard `net/http` handlers only at integration edges

Prometheus `promhttp.Handler` will be registered through `adaptor.HertzHandler`. Static upload GET requests will use the same supported adaptor around a standard `http.FileServer`, with the `/uploads` prefix handled explicitly and directory `Readdir` neutralized to match Gin's disabled listing behavior. HEAD requests will run that same `http.FileServer` against a metadata-only response writer and copy the resulting status and headers into Hertz, because Hertz v0.10.5's streaming adaptor forces the HEAD `Content-Length` response header to zero.

**Rationale:** Prometheus and standard file serving already provide correct, maintained `net/http` implementations. The official Hertz adaptor minimizes custom compatibility code for response bodies and preserves HTTP byte-range semantics required for video playback. Reusing the exact file server for HEAD preserves directory redirects/listings, range, conditional request, modification time, and MIME sniffing semantics without writing a response body.

**Trade-off:** The adaptor has some performance cost. It is limited to metrics and local static files, not normal API handlers.

### 6. Configure uploads for bounded, non-retained large bodies

The Hertz server will switch large bodies to streaming mode and avoid retaining large request buffers. Automatic multipart pre-parsing will be disabled so the upload handler can enforce the existing 1 GiB hard ceiling with a limited request reader before using multipart temp-file behavior. The upload handler will retain the existing per-kind limits, extension and MIME checks, filename generation, target directories, video metadata validation, faststart processing, and cleanup on failures.

Oversized-body behavior will be covered at both the server boundary and handler validation boundary so requests cannot bypass limits through missing or misleading content length.

**Rationale:** A mechanical `http.MaxBytesReader` replacement is not available because Hertz does not expose Gin's `Writer` and `*http.Request` model. A limited request reader plus existing file-level validation provides defense in depth without weakening memory behavior.

### 7. Recreate request metrics with Hertz route metadata

The HTTP metrics middleware will execute the downstream handler chain, read the normalized matched route, request method, and final status from Hertz, then update the existing Prometheus collectors. Raw request paths will remain a fallback only when no normalized route is available.

**Rationale:** Stable route labels prevent unbounded Prometheus cardinality and preserve current dashboard queries.

### 8. Use Hertz's in-process test utilities

API-flow tests will construct Hertz servers and call `pkg/common/ut.PerformRequest`. Shared test helpers will translate request bodies and headers into Hertz test inputs while preserving existing status, JSON body, header, idempotency, authentication, pagination, upload, and state-change assertions.

The static range test will continue to assert `206 Partial Content`, `Content-Range`, and the selected response bytes.

**Rationale:** Hertz does not implement `net/http.Handler`, so Gin's `httptest.NewRecorder` plus `ServeHTTP` pattern is not the native test boundary.

## Risks / Trade-offs

- **[Automated rewrite produces compiling but incorrect behavior]** → Treat script output as untrusted draft work, review every Gin-coupled file, and rely on behavior-focused tests.
- **[Hertz binding differs from Gin for malformed or empty JSON]** → Preserve explicit error mapping and add compatibility cases for representative endpoints before removing Gin.
- **[Large multipart requests increase memory or disk pressure]** → Configure body ceilings and buffer retention, verify temp-file cleanup, and retain per-kind size checks.
- **[Static adaptor changes path or Range behavior]** → Use explicit prefix handling and retain the static byte-range integration test.
- **[Metric labels change or become high-cardinality]** → Assert normalized route labels and existing collector names in middleware tests.
- **[Middleware context values are lost]** → Centralize typed identity lookup helpers and cover required, optional, and internal authentication flows.
- **[Framework defaults change redirects or method handling]** → Configure router options to match current behavior and add route-contract tests where defaults differ.
- **[One-step cutover creates a large diff]** → Stage commits by runtime, middleware/handlers, integrations, tests, dependency removal, and documentation while keeping the branch rollbackable.

## Migration Plan

1. Record the current Go test baseline and route inventory.
2. Add Hertz dependencies and create the Hertz runtime package with address, body-size, recovery, logging, and metrics configuration.
3. Convert route registration, middleware, and handlers while preserving application service boundaries.
4. Adapt Prometheus and static file serving; migrate multipart upload handling and its limits.
5. Convert shared API test helpers and all module API-flow tests to Hertz utilities.
6. Remove Gin dependencies and Gin-specific packages after repository-wide searches show no remaining runtime references.
7. Update architecture, engineering, optimization, quick-read, README, interview material, Copilot instructions, and OpenSpec project context.
8. Run targeted upload/static/auth tests, the complete Go test suite, Go builds, Compose validation, and strict OpenSpec validation.

Rollback is branch-level until the migration is merged. No data migration or irreversible storage change is involved; reverting the migration commits restores the Gin runtime.

## Open Questions

None. Implementation may select the exact compatible Hertz patch release after dependency resolution, but it must satisfy the behavior and testing requirements in this change.
