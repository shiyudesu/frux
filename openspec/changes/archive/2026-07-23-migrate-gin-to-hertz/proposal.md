## Why

GCFeed's HTTP process is coupled to Gin across routing, handlers, middleware, observability, static file serving, uploads, and API-flow tests. Migrating this boundary to CloudWeGo Hertz modernizes the HTTP runtime while preserving the existing layered architecture and externally observable API behavior.

## What Changes

- Replace the Gin engine and startup path with a Hertz server configured from the existing API port.
- Convert HTTP handlers and middleware to Hertz handler signatures while continuing to pass the standard request `context.Context` into application services.
- Replace Gin request binding, response helpers, route groups, authentication context storage, and request metadata access with Hertz equivalents.
- Adapt Prometheus's `promhttp.Handler` and local static upload serving through Hertz's supported `http.Handler` adaptor.
- Rework multipart upload size enforcement so the existing upload limits, validation, cleanup, video probing, and faststart behavior remain intact.
- Migrate API-flow tests from Gin's `ServeHTTP` test pattern to Hertz's in-process request utilities.
- Remove the Gin dependency and rename Gin-specific infrastructure packages and documentation references.
- Preserve all existing public and internal routes, methods, payloads, status codes, headers, authentication rules, idempotency behavior, cursor behavior, static file URLs, byte-range responses, health responses, and Prometheus metric names.

## Capabilities

### New Capabilities

- `hertz-http-runtime`: Defines the Hertz-based API runtime and the compatibility guarantees required during the framework migration.

### Modified Capabilities

None.

## Impact

- **Backend code:** `apps/api/cmd/feed`, `internal/infra/httpgin`, HTTP metrics middleware, all packages under `internal/interfaces/http`, and API-flow tests under `apps/api/test`.
- **Dependencies:** remove `github.com/gin-gonic/gin`; add the current compatible CloudWeGo Hertz modules and use the official Hertz `http.Handler` adaptor.
- **Runtime behavior:** the API process changes HTTP framework, but its external contract remains compatible. The worker process and all domain, application, persistence, cache, messaging, JWT, and frontend behavior remain unchanged.
- **Documentation:** update the project technology baseline, architecture diagrams, engineering guide, optimization notes, quick-read material, README, OpenSpec project context, and other Gin-specific references.
- **Delivery risk:** the official Gin migration script is used only as an optional, pinned, reviewable draft generator. Unsupported bindings, request contexts, middleware, uploads, static files, metrics, startup, and tests require deliberate migration and verification.
