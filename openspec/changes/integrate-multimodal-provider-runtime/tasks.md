## 1. Runtime Configuration and Protocol Types

- [x] 1.1 Extend multimodal provider configuration with endpoint, HMAC secret, protocol version,
  loopback HTTP opt-in, startup timeout, and encoded request/response byte limits while preserving
  disabled defaults and environment interpolation.
- [x] 1.2 Define infrastructure-owned readiness, video, query, contract, image, result, and closed
  error envelopes without changing domain/application provider interfaces.
- [x] 1.3 Add configuration tests for disabled mode, HTTPS/loopback rules, secret and size bounds,
  partial configuration, and process-scoped provider requirements.

## 2. Authenticated HTTP Provider Adapter

- [x] 2.1 Implement one reusable HTTP client with redirect rejection, bounded connection settings,
  context propagation, canonical request HMAC, response HMAC verification, and bounded body reads.
- [x] 2.2 Implement signed readiness inspection with protocol, capability, and complete immutable
  contract validation.
- [x] 2.3 Implement video-content request serialization after image count/size/pixel/MIME/digest
  validation, with no URLs, credentials, user IDs, or behavior metadata.
- [x] 2.4 Implement query-text request serialization and shared strict response decoding, source/
  contract/vector validation, and one-attempt retryable versus terminal failure mapping.
- [x] 2.5 Add fixed-cardinality adapter observations for operation/result without raw bodies, query
  text, image bytes, vectors, identifiers, endpoints, secrets, or arbitrary errors as labels.

## 3. Conformance and Security Tests

- [x] 3.1 Add an `httptest` conformance server that independently verifies request signatures and
  payload shape and returns deterministic normalized vectors only for tests.
- [x] 3.2 Test readiness, video, and query success plus exact source, contract, digest, dimension,
  norm, operation-ID, and response-signature validation.
- [x] 3.3 Test timeout, cancellation, redirect, oversized request/response, malformed JSON, unknown
  fields, rate limit, retryable server status, terminal rejection, and unreachable endpoint mapping.
- [x] 3.4 Add privacy assertions proving payload and observability boundaries and proving the
  conformance server is unavailable as a runtime model implementation.

## 4. Worker Runtime Wiring

- [x] 4.1 Add a composition helper that constructs and readiness-validates the provider only when
  Worker video-job execution is enabled.
- [x] 4.2 Construct the FFmpeg multimodal media preparer and existing job worker from repositories,
  media store, provider, and parsed bounded configuration.
- [x] 4.3 Run the multimodal job worker under Worker process cancellation/shutdown, preserve durable
  handoff independence, and surface fatal executor startup/runtime errors through supervision.
- [x] 4.4 Add composition tests for disabled startup, compatible runtime, contract/capability mismatch,
  provider outage, and no job claim before readiness succeeds.

## 5. API Query and Discovery Wiring

- [x] 5.1 Construct and readiness-validate the provider only when query embedding or hybrid search is
  enabled; preserve provider-free similar-video-only mode.
- [x] 5.2 Construct the bounded query cache/embedder and install the existing hybrid video-search
  option with the exact index, readable-video loader, configured merge policy, and shared contract.
- [x] 5.3 Keep similar-video construction flag-scoped and ensure disabled/search-fallback behavior
  remains unchanged when runtime inference is unavailable after startup.
- [x] 5.4 Add router/API-flow tests for enabled hybrid wiring, first-page lexical degradation,
  hybrid continuation failure, similar-only startup, and fully disabled startup.

## 6. Configuration Surfaces and Documentation

- [ ] 6.1 Add inactive local, Docker, and production configuration fields and environment bindings
  without committing an endpoint, secret, model identity, or enabled multimodal flag.
- [ ] 6.2 Document the provider protocol, startup handshake, security boundary, failure mapping,
  process wiring, conformance workflow, activation order, and rollback.
- [ ] 6.3 Update the recommendation roadmap to mark the transport/runtime prerequisite complete only
  after implementation, while keeping concrete model selection and semantic quality evaluation next.

## 7. Verification

- [ ] 7.1 Run targeted config, provider, Worker, query, search, router, metrics, privacy, and race tests.
- [ ] 7.2 Run `cd apps/api && go test ./...`, `go vet ./...`, and build `./cmd/feed`, `./cmd/worker`,
  and `./cmd/multimodal-eval`.
- [ ] 7.3 Validate local/production Compose and deployment manifests without a configured provider,
  then run `openspec validate --all --strict` and confirm no model runtime, training, historical
  backfill, ANN index, or recommendation activation was added.
