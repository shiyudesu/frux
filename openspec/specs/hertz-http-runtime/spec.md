# hertz-http-runtime Specification

## Purpose

Defines the CloudWeGo Hertz HTTP runtime and the compatibility, security, observability, static-file, upload, and test guarantees required at the GCFeed API boundary.

## Requirements

### Requirement: Hertz API Process

The GCFeed API process SHALL use CloudWeGo Hertz as its production HTTP server and SHALL NOT retain Gin as a runtime dependency.

#### Scenario: API process starts

- **WHEN** the feed API binary starts with a valid configuration and database connection
- **THEN** it listens on the configured API port using Hertz and registers the complete API route tree

#### Scenario: Repository dependencies are resolved

- **WHEN** the backend module dependencies are inspected after migration
- **THEN** Hertz is present and `github.com/gin-gonic/gin` is absent

### Requirement: HTTP Contract Compatibility

The Hertz route tree SHALL preserve the existing public, internal, health, metrics, upload, and static-file HTTP contracts.

#### Scenario: Existing route is requested

- **WHEN** a client calls an existing route with the same method, path, parameters, headers, and body used before migration
- **THEN** the request reaches the same application use case and returns a compatible status code, headers, and response payload

#### Scenario: Invalid JSON is submitted

- **WHEN** a JSON endpoint receives a malformed or incompatible request body
- **THEN** it returns the endpoint's existing concise `400 Bad Request` error payload without exposing framework internals

#### Scenario: Unknown resource is requested

- **WHEN** a client requests a route or resource that does not exist
- **THEN** Hertz preserves the existing not-found behavior expected by the API-flow tests

### Requirement: Request Context and Authentication

Hertz handlers SHALL pass the standard request `context.Context` to application services and SHALL keep authentication identity request-scoped.

#### Scenario: Authenticated request reaches a service

- **WHEN** a valid access token is accepted by the JWT middleware
- **THEN** the downstream handler can read the authenticated user ID and role and passes the standard request context to its application service

#### Scenario: Required authentication is missing

- **WHEN** a protected endpoint is called without a valid access token
- **THEN** middleware stops the handler chain and returns the existing unauthorized response

#### Scenario: Optional authentication is invalid

- **WHEN** a public feed endpoint receives a missing or invalid optional access token
- **THEN** the request continues without an authenticated viewer identity

#### Scenario: Internal authentication is invalid

- **WHEN** an internal endpoint receives a missing or incorrect `X-Internal-Token`
- **THEN** middleware stops the handler chain and returns the existing unauthorized response

### Requirement: Health and Prometheus Integration

The Hertz API process SHALL preserve the health endpoint and existing Prometheus metric contract.

#### Scenario: Health is queried

- **WHEN** a client sends `GET /health`
- **THEN** the API returns `200 OK` with the existing health response payload

#### Scenario: Metrics are scraped

- **WHEN** Prometheus sends `GET /metrics`
- **THEN** the API exposes the registered Prometheus collectors through Hertz

#### Scenario: Request metrics are recorded

- **WHEN** Hertz completes an API request
- **THEN** the existing request count and duration metrics are updated with method, normalized route, and final status labels

### Requirement: Static Upload Serving

The Hertz API process SHALL serve files from the existing uploads root at `/uploads` and SHALL preserve HTTP byte-range behavior.

#### Scenario: Uploaded file is requested

- **WHEN** a client sends a GET request for an existing `/uploads/{kind}/{filename}` resource
- **THEN** the server returns the file using the existing public URL structure

#### Scenario: Video byte range is requested

- **WHEN** a client sends a valid `Range` request for an uploaded video
- **THEN** the server returns `206 Partial Content`, a correct `Content-Range` header, and only the requested bytes

#### Scenario: Static metadata is requested

- **WHEN** a client sends a HEAD request for an existing uploaded resource
- **THEN** the server returns compatible headers without returning the resource body

### Requirement: Bounded Multipart Uploads

The Hertz upload path SHALL enforce the existing total and per-kind upload limits without retaining an entire maximum-sized video request in memory.

#### Scenario: Valid upload is submitted

- **WHEN** an authenticated client uploads a supported file within the applicable limits
- **THEN** the server validates, processes, saves, and returns the same upload response fields and URL format as before migration

#### Scenario: Upload exceeds a configured limit

- **WHEN** an upload exceeds the total limit or its video, cover, or avatar limit
- **THEN** the request is rejected and the oversized file is not persisted

#### Scenario: Video processing fails

- **WHEN** video validation or faststart processing fails after a temporary target has been created
- **THEN** the server removes the incomplete target and returns the existing class of client or server error

### Requirement: Hertz API-Flow Test Coverage

The migrated HTTP boundary SHALL be exercised through Hertz's in-process request utilities with behavior coverage equivalent to the existing Gin API-flow tests.

#### Scenario: Module API tests run

- **WHEN** the backend Go test suite executes
- **THEN** account, video, feed, interaction, relation, message, recommendation, exposure, playback, upload, authentication, idempotency, pagination, and error-flow tests invoke Hertz routes without opening a network listener

#### Scenario: Static range regression test runs

- **WHEN** the static upload range test executes
- **THEN** it verifies partial-content status, content-range metadata, and response bytes through the Hertz route tree
