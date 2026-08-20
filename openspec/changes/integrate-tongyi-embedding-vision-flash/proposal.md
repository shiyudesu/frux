## Why

Frux now has a secure model-neutral provider boundary, but semantic features still cannot produce real
vectors. The selected `tongyi-embedding-vision-flash-2026-03-06` model supports Chinese text, images,
multi-image input, fused embeddings, and a fixed 768-dimensional output suitable for the existing
exact-search and hybrid-discovery pipeline.

## What Changes

- Add a standalone Go adapter service that implements `frux-multimodal-v1` and calls Alibaba Cloud
  Model Studio's native multimodal-embedding HTTP API with a configured API key and endpoint.
- Fix the active model profile to provider `alibaba-bailian`, model
  `tongyi-embedding-vision-flash`, revision `2026-03-06-res1`, dimension 768, and resolution level 1.
- Translate video requests into one fused `text + multi_images` content object using Base64 Data URIs,
  and translate public queries into text embedding requests in the same model space.
- Strictly validate upstream status, JSON shape, result count/type, vector dimension/components, and
  usage metadata; normalize vectors, calculate the Frux vector digest, and sign the downstream response.
- Verify credentials and the selected model with a bounded upstream probe before the adapter becomes
  ready, while keeping API keys and upstream response bodies out of Frux-facing payloads and logs.
- Add deterministic fake-upstream integration tests, configuration examples, container build support,
  usage/cost metrics, and operator documentation. Existing multimodal feature flags remain disabled.

## Capabilities

### New Capabilities

- `tongyi-multimodal-provider`: Model-specific translation, startup validation, usage accounting,
  security, and operations for Tongyi Embedding Vision Flash.

### Modified Capabilities

- `multimodal-provider-runtime`: Define how a concrete adapter proves readiness and returns validated
  model vectors through the existing signed Frux protocol.
- `multimodal-video-embeddings`: Select the first immutable development model contract and native
  text-plus-keyframe fusion profile without enabling production traffic.

## Impact

- Adds `cmd/multimodal-provider` and supporting infrastructure code, but no Python runtime or model
  weights.
- Adds environment variables for the DashScope endpoint/API key and adapter listen/security settings.
- Updates the API container build to include the adapter binary; deployment remains opt-in.
- Makes real provider calls billable only when the standalone adapter is started and Frux multimodal
  feature flags are explicitly enabled.
