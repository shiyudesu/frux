## Why

The multimodal embedding, job, exact-search, hybrid-search, and similar-video paths now exist, but
they remain deliberately disabled because no production-shaped provider transport is constructed or
wired into the API and Worker processes. Frux needs a secure, bounded, model-neutral runtime adapter
before any concrete pretrained model can be evaluated or enabled without coupling Go application
code to a model SDK or deployment topology.

## What Changes

- Add a Frux-owned versioned HTTP inference protocol for video-content and query-text embeddings,
  including strict request/response limits, immutable contract identity, source binding, and
  deterministic error classes.
- Add a Go HTTP provider adapter with authenticated requests, response authentication, bounded body
  reads, redirect rejection, transport security validation, deadline propagation, and privacy-safe
  observability.
- Construct the provider once per process and wire it into the Worker job executor and API query
  embedder, hybrid search, and similar-video paths without changing domain/application interfaces.
- Add startup compatibility and readiness probes so a process cannot enable a path against a provider
  that reports a different protocol or model contract.
- Add a deterministic in-process conformance server for integration and failure-path tests; it is a
  transport fixture, not a fake model used for product-quality claims.
- Keep all multimodal feature flags disabled by default. This change does not choose, package, train,
  download, or activate a concrete model and does not claim semantic quality.

## Capabilities

### New Capabilities

- `multimodal-provider-runtime`: Secure provider protocol, client lifecycle, compatibility checks,
  process wiring, failure mapping, and conformance testing for an external multimodal inference
  runtime.

### Modified Capabilities

- `multimodal-video-embeddings`: Require enabled video jobs to execute through a validated compatible
  runtime and preserve job fencing, source revalidation, and bounded failure behavior.
- `multimodal-video-discovery`: Require enabled query/hybrid paths to use the same validated runtime
  contract as stored video vectors while preserving lexical fallback and exact retrieval behavior.

## Impact

- Affects multimodal configuration, API and Worker composition roots, embedding infrastructure,
  search service wiring, metrics, tests, local/deployment configuration examples, and operational
  documentation.
- Adds no provider SDK to domain or application packages and no mandatory Python or model dependency
  to the Go build.
- Introduces an internal HTTP protocol that a later concrete local or remote model service must
  implement before multimodal flags can be enabled.
