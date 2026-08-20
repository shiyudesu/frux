## Why

Frux now has a secure model-neutral provider boundary, but semantic features still cannot produce real
vectors. Alibaba Cloud exposes both the dated `tongyi-embedding-vision-flash-2026-03-06` snapshot and
the undated `tongyi-embedding-vision-flash` model. Both produce 768-dimensional cross-modal vectors,
but only the dated snapshot supports native fused embeddings, so the adapter must select an explicit
capability-aware profile instead of hard-coding one upstream model.

## What Changes

- Add a standalone Go adapter service that implements `frux-multimodal-v1` and calls Alibaba Cloud
  Model Studio's native multimodal-embedding HTTP API with a configured API key and endpoint.
- Add an allowlisted model-profile setting shared by the adapter and Frux configuration. Initially
  support `tongyi-embedding-vision-flash-2026-03-06` and `tongyi-embedding-vision-flash` without
  accepting arbitrary, unverified model IDs.
- For the dated snapshot, translate video requests into one native fused `text + multi_images`
  content object. For the undated model, request independent `text` and `multi_images` vectors and
  combine their normalized values with a deterministic equal-weight mean.
- Give each profile a distinct immutable Frux contract so native-fused and locally-fused vectors are
  never stored, queried, or compared as though they came from the same embedding process.
- Strictly validate upstream status, JSON shape, result count/type, vector dimension/components, and
  usage metadata; normalize vectors, calculate the Frux vector digest, and sign the downstream response.
- Verify credentials and the selected model with a bounded upstream probe before the adapter becomes
  ready, while keeping API keys and upstream response bodies out of Frux-facing payloads and logs.
- Add deterministic fake-upstream integration tests, configuration examples, container build support,
  usage/cost metrics, and operator documentation. Existing multimodal feature flags remain disabled.
- Automatically discover a repository-local `.env.multimodal` for native development while loading
  only process-appropriate variables and preserving already-injected environment values.

## Capabilities

### New Capabilities

- `tongyi-multimodal-provider`: Model-specific translation, startup validation, usage accounting,
  security, and operations for Tongyi Embedding Vision Flash.

### Modified Capabilities

- `multimodal-provider-runtime`: Define how a concrete adapter proves readiness and returns validated
  model vectors through the existing signed Frux protocol.
- `multimodal-video-embeddings`: Select one of the supported immutable development contracts and
  bind each contract to its model-specific text-plus-keyframe fusion policy without enabling traffic.

## Impact

- Adds `cmd/multimodal-provider` and supporting infrastructure code, but no Python runtime or model
  weights.
- Adds environment variables for the selected model profile, DashScope endpoint/API key, and adapter
  listen/security settings.
- Updates the API container build to include the adapter binary; deployment remains opt-in.
- Makes real provider calls billable only when the standalone adapter is started and Frux multimodal
  feature flags are explicitly enabled.
