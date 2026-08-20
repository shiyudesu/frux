## Context

Frux exposes a signed `frux-multimodal-v1` provider protocol and wires it into API query embedding
and Worker video-job execution. The remaining gap is a concrete provider process. The selected
Alibaba Cloud Model Studio snapshot accepts text, image, video, and `multi_images` content, supports
fusion when fields share one content object, and can return 768-dimensional vectors.

The Frux Worker already prepares bounded image bytes locally. Sending those bytes as Base64 Data URIs
avoids exposing object-store URLs or credentials. The adapter is a separate Go binary so the Frux API
and Worker remain vendor-neutral and the DashScope API key has a narrow deployment boundary.

## Goals / Non-Goals

**Goals:**

- Implement the existing signed Frux provider protocol for the selected Tongyi snapshot.
- Translate video content into one fused text-plus-`multi_images` DashScope request and queries into
  one text request.
- Fix and report one immutable 768-dimensional development contract.
- Validate the upstream response, normalize the vector, calculate its digest, and return a signed
  Frux response.
- Fail startup if a bounded real upstream probe cannot authenticate, invoke the selected model, and
  return a valid vector.
- Expose low-cardinality request/result and input-token metrics without content or credential labels.

**Non-Goals:**

- Hosting model weights or adding Python, CUDA, ROCm, ONNX, or a vendor SDK.
- Sending original video files or storage URLs; the first version uses existing deterministic frames.
- Enabling multimodal API/Worker feature flags by default.
- Historical backfill, ANN indexing, recommendation activation, or claims of quality improvement
  before the real Golden Set is evaluated.

## Decisions

### 1. Add a standalone Go adapter binary

`cmd/multimodal-provider` will run a small `net/http` server. It terminates the Frux HMAC protocol and
uses a reusable HTTP client to call the configurable DashScope native multimodal endpoint with
`Authorization: Bearer <DASHSCOPE_API_KEY>`. No DashScope SDK is added.

This keeps the dependency surface small, makes request/response validation explicit, and allows the
same binary to run locally or behind TLS on another host. Calling DashScope directly from API and
Worker was rejected because it would duplicate vendor logic and bypass the existing provider boundary.

### 2. Freeze one model profile

The adapter reports this contract:

- provider alias: `alibaba-bailian`
- model alias: `tongyi-embedding-vision-flash`
- revision alias: `2026-03-06-res1`
- dimension: `768`
- existing Frux canonicalizer/frame/preprocessing/fusion policy IDs

The upstream model ID is `tongyi-embedding-vision-flash-2026-03-06`, with `dimension=768`,
`output_type=dense`, and `res_level=1`. Resolution level is encoded into the revision alias so a later
change cannot silently mix vectors produced with another visual tokenization profile.

### 3. Use native fusion for video content

A video request becomes one `input.contents` element containing `text` and `multi_images`. Every image
is converted to `data:<mime>;base64,<bytes>`. This asks the dated Tongyi snapshot for one `type=fused`
vector. Query requests use one `{text: ...}` content element and require one `type=text` vector.

The adapter does not send `enable_fusion`; official behavior for the dated Tongyi models derives
fusion from multiple modality fields in one content object. It does not send a raw video URL.

### 4. Normalize and bind every upstream result

The upstream response must contain exactly one embedding at index zero with the expected type and
768 finite non-zero components. The adapter L2-normalizes the vector, computes the Frux digest, and
binds it to the request source hash and fixed contract before responding. Unknown JSON fields are
accepted from DashScope for forward-compatible metadata, but required output and usage fields are
strictly validated and the response body is size-bounded.

### 5. Make readiness model-backed

Before serving, the command submits one small text embedding probe to DashScope. Startup fails for
invalid credentials, wrong endpoint/model, non-768 output, malformed responses, or timeout. After a
successful probe, `/v1/ready` answers locally with the fixed contract and video/query capabilities;
normal calls still surface later upstream failures through signed closed errors.

The probe incurs a negligible billable request but prevents API/Worker from accepting an adapter
that has never demonstrated access to the selected model.

### 6. Keep upstream configuration secret and explicit

The command reads configuration from environment variables. The API key and HMAC secret are required;
the upstream endpoint is required rather than guessed because Alibaba Cloud endpoints can be global
or workspace/region specific. The listen address, upstream timeout, body limits, and graceful-shutdown
timeout have bounded defaults.

No API key, upstream endpoint, raw provider body, request text, image bytes, vector, source hash,
operation ID, or Alibaba request ID is emitted as a metric label or normal log field.

## Risks / Trade-offs

- [External API availability and billing] → Keep all Frux feature flags off by default, preserve
  lexical fallback and durable retries, and expose input-token counters from successful responses.
- [Base64 expands image payloads] → Reuse existing small frame limits and enforce independent inbound
  and upstream request byte caps.
- [Provider may alter undocumented response details] → Validate required fields while tolerating
  unrelated extra upstream metadata; pin the dated model ID and explicit dimension/resolution.
- [Normalized vectors differ slightly from raw output] → Make normalization part of this immutable
  adapter revision and verify deterministic digest generation.
- [A startup probe makes deployment require network access] → Bound it with a short timeout and keep
  disabled deployments free of this service.

## Migration Plan

1. Build and test the adapter against a fake DashScope server.
2. Add the binary to the existing API image and document a native/local launch command.
3. Configure `DASHSCOPE_MULTIMODAL_ENDPOINT`, `DASHSCOPE_API_KEY`, and the shared Frux provider HMAC
   secret only in the adapter process.
4. Point API and Worker at the adapter and configure the fixed contract, initially keeping all
   multimodal feature flags false.
5. Start the adapter, verify readiness, create controlled development videos, and run the real Golden
   Set before enabling Similar, Hybrid, or video jobs.

Rollback stops the adapter and disables multimodal flags. Existing vectors are retained under their
immutable contract and all lexical/hash/Feed behavior continues.

## Open Questions

- The exact Alibaba Cloud endpoint depends on the user's region/workspace and must be supplied at
  deployment time.
- The real API key is intentionally not required for repository tests; final live verification waits
  until the user provides it through their local environment.
