## Context

Frux exposes a signed `frux-multimodal-v1` provider protocol and wires it into API query embedding
and Worker video-job execution. The remaining gap is a concrete provider process. Alibaba Cloud's
dated Tongyi Flash snapshot accepts text and `multi_images` in one fused content object, while the
undated Tongyi Flash model accepts the same modalities only as independent inputs. Both return fixed
768-dimensional vectors for the profiles used here.

The Frux Worker already prepares bounded image bytes locally. Sending those bytes as Base64 Data URIs
avoids exposing object-store URLs or credentials. The adapter is a separate Go binary so the Frux API
and Worker remain vendor-neutral and the DashScope API key has a narrow deployment boundary.

## Goals / Non-Goals

**Goals:**

- Implement the existing signed Frux provider protocol for the supported Tongyi Flash profiles.
- Select the upstream model through one explicit allowlisted profile setting shared by adapter and
  Frux runtime configuration.
- Use native fusion for the dated snapshot and deterministic local fusion for the undated model.
- Report a distinct immutable 768-dimensional development contract for each profile.
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

### 2. Select one verified model profile

`FRUX_MULTIMODAL_PROFILE` selects one closed profile definition rather than accepting arbitrary model
and contract strings:

| Profile | Upstream model | Revision | Fusion policy |
| --- | --- | --- | --- |
| `tongyi-embedding-vision-flash-2026-03-06` | same as profile | `2026-03-06-res1` | `provider-fusion-v1` |
| `tongyi-embedding-vision-flash` | same as profile | `stable-independent-mean-v1` | `normalized-mean-fusion-v1` |

Both contracts use provider `alibaba-bailian`, model alias `tongyi-embedding-vision-flash`, dimension
768, and the existing canonicalization, frame-sampling, and image-preprocessing policies. The dated
profile also fixes `res_level=1`. Keeping the mapping in one shared package prevents the API/Worker
contract and adapter behavior from drifting independently.

The undated provider name can be repointed by Alibaba Cloud. Its distinct revision alias makes the
risk visible; an observed provider change requires a new Frux revision and vector regeneration before
the deployment is considered stable.

### 3. Use profile-specific video fusion

Every image is converted to `data:<mime>;base64,<bytes>` and no raw video URL is sent.

For `tongyi-embedding-vision-flash-2026-03-06`, a video request becomes one `input.contents` element
containing `text` and `multi_images`. The adapter requires one index-zero `type=fused` vector and does
not send `enable_fusion`.

For `tongyi-embedding-vision-flash`, a video request becomes two ordered content elements: one
`{text: ...}` and one `{multi_images: [...]}`. The adapter requires index-zero `type=text` and
index-one `type=multi_images`, L2-normalizes both, computes their equal-weight arithmetic mean, then
L2-normalizes the result. This algorithm is part of `normalized-mean-fusion-v1`; changing weights or
normalization creates a new fusion-policy identifier.

Query requests always use one `{text: ...}` content element and require one `type=text` vector from
the selected upstream model.

### 4. Normalize and bind every upstream result

The upstream response must contain exactly the count, indexes, and types required by the active
profile, with 768 finite non-zero components per vector. The adapter normalizes the selected or fused
vector, computes the Frux digest, and binds it to the request source hash and selected contract before
responding. Unknown JSON fields are accepted from DashScope for forward-compatible metadata, but
required output and usage fields are strictly validated and the response body is size-bounded.

### 5. Make readiness model-backed

Before serving, the command submits one small text embedding probe to DashScope. Startup fails for
invalid credentials, wrong endpoint/model, non-768 output, malformed responses, or timeout. After a
successful probe, `/v1/ready` answers locally with the selected contract and video/query capabilities;
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

For native repository development, API, Worker, and Adapter commands discover the nearest
`.env.multimodal` within the repository so operators do not need to run `source` for every shell.
Existing process environment values take precedence. API and Worker load only the Frux profile,
provider endpoint, and shared HMAC secret; only the Adapter loads DashScope credentials and Tongyi
upstream settings. Missing files are ignored, but a discovered malformed file fails startup.

## Risks / Trade-offs

- [External API availability and billing] → Keep all Frux feature flags off by default, preserve
  lexical fallback and durable retries, and expose input-token counters from successful responses.
- [Base64 expands image payloads] → Reuse existing small frame limits and enforce independent inbound
  and upstream request byte caps.
- [Provider may alter response details or repoint the undated model] → Validate profile-specific
  result shapes, prefer the dated snapshot for reproducibility, and require a new contract revision
  before accepting changed undated-model behavior.
- [Local fusion quality differs from provider-native fusion] → Keep the policies and vector stores
  separate and compare both profiles on the Golden Set before enabling semantic discovery.
- [Normalized vectors differ slightly from raw output] → Make normalization part of this immutable
  adapter revision and verify deterministic digest generation.
- [A startup probe makes deployment require network access] → Bound it with a short timeout and keep
  disabled deployments free of this service.

## Migration Plan

1. Build and test the adapter against a fake DashScope server.
2. Add the binary to the existing API image and document a native/local launch command.
3. Configure `FRUX_MULTIMODAL_PROFILE`, `DASHSCOPE_MULTIMODAL_ENDPOINT`, `DASHSCOPE_API_KEY`, and the
   shared Frux provider HMAC secret. Only the adapter process receives the API key.
4. Point API and Worker at the adapter; their shared profile setting resolves the same exact contract,
   initially keeping all multimodal feature flags false.
5. Start the adapter, verify readiness, create controlled development videos, and run the real Golden
   Set before enabling Similar, Hybrid, or video jobs.

Rollback stops the adapter and disables multimodal flags. Existing vectors are retained under their
immutable contract and all lexical/hash/Feed behavior continues.

## Open Questions

- The exact Alibaba Cloud endpoint depends on the user's region/workspace and must be supplied at
  deployment time.
- The real API key is intentionally not required for repository tests; final live verification waits
  until the user provides it through their local environment.
