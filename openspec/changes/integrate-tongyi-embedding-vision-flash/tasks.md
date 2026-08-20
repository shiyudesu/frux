## 1. Initial Dated Model Contract and Adapter Configuration

- [x] 1.1 Define the immutable Tongyi Flash model/profile constants and contract constructor.
- [x] 1.2 Add strict environment configuration for listen address, shared HMAC secret, DashScope
  endpoint/API key, upstream timeout, body limits, and graceful shutdown.
- [x] 1.3 Add configuration tests proving secrets are required, endpoints are HTTPS, limits are
  bounded, and the selected model/dimension/resolution cannot drift.

## 2. DashScope Client

- [x] 2.1 Implement a reusable non-redirecting Bearer-authenticated HTTP client for the native
  multimodal-embedding endpoint with one attempt and bounded response reads.
- [x] 2.2 Translate query requests to one text content item and video requests to one fused
  `text + multi_images` content object using validated Base64 Data URIs.
- [x] 2.3 Validate upstream status, exact result count/index/type, 768 finite non-zero components,
  normalize the vector, and parse bounded usage counters.
- [x] 2.4 Map timeout/network/408/429/5xx failures to retryable errors and all other upstream or
  validation failures to terminal closed errors without exposing raw bodies.

## 3. Frux Provider Server

- [x] 3.1 Implement strict signed request verification, clock-skew/replay-envelope checks, body
  limits, contract/source/image validation, and signed closed error responses.
- [x] 3.2 Implement `/v1/ready`, `/v1/embed/video`, `/v1/embed/query`, and a detail-free `/health`
  liveness endpoint using the fixed contract and capabilities.
- [x] 3.3 Run a real bounded text probe before listening so invalid API keys, endpoint/model failures,
  and incompatible vectors prevent readiness.
- [x] 3.4 Add fixed-cardinality adapter operation/result metrics and aggregate text/image/input token
  counters without content, identifiers, endpoint, model, or error labels.

## 4. Tests and Process Lifecycle

- [x] 4.1 Add fake-DashScope tests for query translation, fused multi-image translation, Bearer auth,
  fixed parameters, valid vectors, normalization, digest, and usage accounting.
- [x] 4.2 Add tests for Frux HMAC validation, stale timestamps, contract/source/image rejection,
  signed responses, and readiness behavior.
- [x] 4.3 Add tests for redirects, timeouts, cancellation, oversized/malformed responses, wrong result
  type/count/index/dimension, non-finite/zero vectors, and upstream status mapping.
- [x] 4.4 Add the standalone command with startup probe, signal-aware server lifecycle, bounded
  shutdown, and no credential/content logging.
- [x] 4.5 Run the existing multimodal projection reconciler under the Worker lifecycle so newly
  persisted Tongyi vectors become eligible for Exact, Similar, and Hybrid retrieval automatically.

## 5. Packaging and Documentation

- [x] 5.1 Build the adapter binary in the existing API image and add inactive environment examples
  without committing a real API key or enabling Frux multimodal flags.
- [x] 5.2 Document the selected contract, DashScope payload mapping, launch/configuration sequence,
  cost/usage observation, feature activation order, and rollback.
- [x] 5.3 Update the recommendation roadmap and model-evaluation instructions without claiming Golden
  Set quality before a real credential-backed run.

## 6. Verification

- [x] 6.1 Run targeted adapter, provider-protocol, configuration, Worker, router, privacy, and race tests.
- [x] 6.2 Run the complete Go tests, vet, command builds, Compose/deployment validation, and strict
  OpenSpec validation.
- [x] 6.3 Confirm all default multimodal flags remain disabled and no API key, generated vector,
  source content, raw upstream body, historical backfill, ANN index, or recommendation activation is committed.

## 7. Configurable Tongyi Model Profiles

- [x] 7.1 Replace the hard-coded model contract with one shared allowlisted profile resolver for the
  dated and undated Tongyi Flash model IDs, including a distinct local-fusion policy identity.
- [x] 7.2 Keep native fusion for the dated snapshot and implement strict independent-result validation
  plus deterministic normalized-mean fusion for the undated model.
- [x] 7.3 Make the profile selectable through configuration, update inactive environment examples and
  operator documentation, and keep all multimodal feature flags disabled by default.
- [x] 7.4 Add profile, payload, response, fusion, contract-isolation, configuration, and regression tests;
  then rerun targeted and complete Go validation before marking the change complete again.
