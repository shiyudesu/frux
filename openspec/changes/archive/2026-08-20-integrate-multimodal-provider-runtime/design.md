## Context

The archived `add-multimodal-video-discovery` change established provider-neutral application
interfaces, durable PostgreSQL jobs, media preparation, vector validation, exact retrieval, query
caching, hybrid search, and similar-video services. The composition roots intentionally pass no
provider: the Worker can create jobs but cannot execute them, and the API cannot construct semantic
query or hybrid-search dependencies. All feature flags therefore remain off.

The next reusable boundary is transport, not a model choice. Frux needs a protocol that can be
implemented by a local process, a container on another machine, or a managed endpoint while keeping
model SDKs and runtime languages out of the Go domain/application layers. The transport carries
prepared public images and normalized public text only; it must not receive storage credentials,
signed URLs, user identity, or behavior data.

## Goals / Non-Goals

**Goals:**

- Define and implement a versioned HTTP protocol for the two existing embedding operations.
- Authenticate request and response bodies and enforce HTTPS except for explicitly allowed loopback
  development endpoints.
- Verify protocol version, supported operations, and the exact immutable model contract before an
  enabled process starts serving or claiming work.
- Wire one provider client per process into the existing Worker job executor and API query/hybrid
  composition without changing domain/application provider interfaces.
- Preserve bounded admission, deadlines, body sizes, shutdown, retries, lexical fallback, job
  fencing, and privacy-safe telemetry.
- Provide deterministic conformance fixtures that exercise the real HTTP adapter and process wiring.

**Non-Goals:**

- Selecting, downloading, packaging, training, quantizing, or serving a concrete pretrained model.
- Adding Python or a model SDK to the Go build.
- Enabling multimodal flags in checked-in default, Docker, or production configuration.
- Backfilling historical videos, adding HNSW/pgvector, or changing recommendation providers.
- Treating deterministic transport fixtures as semantic-quality evidence.

## Decisions

### 1. Use a Frux-owned HTTP protocol instead of a vendor SDK

The infrastructure adapter will implement the existing `MultimodalEmbeddingProvider` interface and
call three endpoints under a versioned base path: readiness/contract inspection, video embedding,
and query embedding. Domain and application packages will continue to know only the existing typed
requests and results.

This keeps local and remote deployment equivalent and avoids allowing a model vendor's payload,
client, or error types to leak across application boundaries. An OpenAI-compatible endpoint was
considered, but its embedding schema does not define Frux's bounded image set, complete compatibility
identity, source hash, or response vector digest.

### 2. Send bounded JSON with inline base64 images

The video endpoint will receive canonical public text plus an ordered list of MIME type, dimensions,
digest, and base64 content. The query endpoint will receive canonical query text. Both carry the
requested immutable contract and source hash. The client will serialize into memory only after the
existing image count, byte, pixel, MIME, and digest checks pass, and it will reject a request whose
encoded body exceeds a hard configured limit.

Multipart streaming was considered, but the existing prepared-image limits are deliberately small
and JSON gives a simpler canonical payload for signatures and conformance suites. URLs are not sent,
which prevents the provider from gaining storage reachability or bearer credentials.

### 3. Authenticate both directions with a canonical HMAC envelope

Requests will include a random operation ID, UTC timestamp, protocol version, and an HMAC-SHA256
signature over protocol version, method, path, timestamp, operation ID, and exact body bytes.
Responses will echo the operation ID and carry an HMAC signature over the exact response body.
Redirects are rejected. Secrets must be 32-512 bytes, never appear in payloads/logs, and are loaded
through existing environment interpolation.

HTTPS is mandatory unless `allow_insecure_local` is explicitly set and the endpoint host is loopback.
This matches the repository's moderation-gateway security posture while keeping the protocol and
keys independent.

### 4. Make readiness a contract handshake, not a generic liveness ping

Before enabling video-job execution or query embedding, the process will call the readiness endpoint
under a short startup timeout. The signed response must report the supported protocol version,
required operation capability, and a contract exactly equal to configured provider/model/revision,
dimension, and preprocessing policies. A mismatch fails startup before jobs are claimed or hybrid
search is exposed.

Similar-video-only mode can continue from already persisted compatible vectors without a live
provider. Runtime outages after startup remain isolated: Worker jobs retry or terminate through the
existing bounded policy, while first-page search falls back to lexical results.

### 5. Keep transport concurrency below the application admission boundary

The HTTP client will be reused per process, reject redirects, cap idle and per-host connections, and
respect caller contexts. The existing Worker and query embedder remain the owners of admission and
deadline policy; the transport adds no queue and no detached retry loop. It performs one HTTP attempt
per application call and surfaces a typed retryability classification.

Status `429`, `502`, `503`, and `504`, transport failures, and response-read failures are retryable.
Other non-success statuses are terminal request/provider rejections. Invalid signatures, oversized
or malformed bodies, incompatible identities, source mismatches, and invalid vectors fail closed and
are classified without returning raw provider bodies.

### 6. Complete composition-root wiring without changing feature defaults

The Worker will construct the HTTP provider, validate readiness, construct the FFmpeg media preparer
and multimodal job worker, and run it under the process context with bounded shutdown. The API will
construct the provider only when query embedding or hybrid search is enabled, then construct the
bounded query cache, query embedder, exact index, hybrid search option, and similar-video service as
required by flags.

Configuration will add endpoint, HMAC secret, protocol version, local-HTTP opt-in, startup timeout,
and request/response byte caps beneath `multimodal.provider`. No checked-in configuration will set a
real endpoint, secret, contract, or enabled feature flag.

### 7. Use a conformance server, not a product mock

Tests will run the real client against an `httptest` server that verifies signatures and exact
payload shape and returns deterministic normalized vectors. It will cover success, mismatched
contract/source, invalid vector, signature failure, oversized response, timeout, rate limit,
retryable server failure, terminal rejection, redirect, and cancellation. Process-composition tests
will prove enabled paths receive dependencies and disabled paths make no provider call.

The fixture is allowed only in tests and explicit developer conformance commands. It must not be
selectable as a runtime model or used in the golden-set report as evidence of semantic relevance.

## Risks / Trade-offs

- [Inline base64 increases memory and payload size] → Keep existing image bounds, add encoded-body
  caps, reject before network I/O, and retain low admission limits.
- [Startup readiness makes enabled processes depend on provider availability] → Use a short bounded
  handshake and keep all flags off by default; once started, preserve runtime degradation behavior.
- [HMAC protects integrity but is not encryption] → Require HTTPS for non-loopback endpoints and
  reject redirects that could disclose signed content.
- [A protocol may constrain a future model service] → Version the protocol, keep the model contract
  explicit, and isolate serialization in one infrastructure package.
- [API and Worker may observe different provider contracts during deployment] → Each process verifies
  the complete configured identity, and persisted vectors remain isolated by contract key.
- [Transport fixtures can look like a working model] → Name and document them as conformance-only and
  keep production feature flags disabled until a real model passes the golden set.

## Migration Plan

1. Add configuration fields and validation while preserving `multimodal.enabled: false` behavior.
2. Add the HTTP adapter, readiness handshake, conformance server, and unit/integration tests.
3. Wire Worker execution and API query/hybrid construction behind existing flags.
4. Validate disabled startup and Compose/deployment manifests with no provider configured.
5. In a later model-specific change, deploy a service that passes conformance, set an immutable
   contract, run the real golden set, and enable development flags incrementally.

Rollback is configuration-first: disable query/hybrid/video-job flags and restart. Lexical search,
existing Feed providers, published videos, hash embeddings, and persisted multimodal facts remain
valid. Removing the adapter code or stored facts is not required for rollback.

## Open Questions

- Which concrete pretrained multimodal model and serving implementation will implement this protocol?
  This decision is intentionally deferred; it blocks semantic-quality evaluation and activation, but
  does not block the transport adapter or process wiring in this change.
