## Context

Frux currently has only in-process fallback content vectors in the recommendation area. A later change, `integrate-semantic-video-embeddings`, needs a stable semantic text contract, but adding Python model dependencies to either Go binary would couple release cadence, memory, and failure behavior to the Feed/worker processes. This change therefore establishes an isolated internal inference service and deliberately leaves all callers, persistence, queues, backfills, and ranking behavior untouched.

The service is optimized for Chinese title/description text, predictable CPU operation, and local/Compose deployment. It is not a general model-serving platform. Its contract must be reproducible enough that later persisted vectors can be keyed by exact model identity and dimension.

## Goals / Non-Goals

**Goals:**

- Add a separately deployable internal Python app with a reproducible locked runtime and immutable model artifact.
- Fix one multilingual model, revision, dimension, normalization mode, and text-composition rule.
- Expose only health, readiness, metadata, and bounded batch embedding endpoints.
- Match Frux internal-token strength and constant-time authentication practices.
- Bound body size, text size, batch size, CPU threads, process count, concurrency, queueing, memory, and time.
- Make startup readiness, overload, errors, logging, container operation, and tests implementation-ready.

**Non-Goals:**

- Calling the service from Go or changing any Go API, worker, feed, or recommendation policy.
- RabbitMQ producers/consumers, PostgreSQL/Redis access, video embedding storage, backfill, pgvector/ANN, or similarity retrieval.
- Dynamic model selection, model administration, GPU support, online downloads, fine-tuning, or recommendation-model training.
- Public/browser endpoints, Web integration, user authentication, or storage of request content.

## Decisions

### 1. Create `apps/semantic-embedding` as an independent Python service

The app will contain `pyproject.toml`, `uv.lock`, source under `src/frux_embedding`, tests, a model-fixture directory, and its own `Dockerfile`. It will use Python `3.12.*`, FastAPI/Pydantic for strict HTTP contracts, one Uvicorn coordinator, a fixed pool of at most two killable inference child processes, Sentence Transformers/PyTorch CPU inference, and `uv` for hash-locked installation. Direct and transitive package versions are committed in `uv.lock`; the final Python base image and `uv` bootstrap image are referenced by digest.

The coordinator owns authentication, validation, admission, deadlines, and response assembly. Native
model execution occurs only in isolated child processes so a hung PyTorch/native kernel can be
terminated without wedging the HTTP process. The service is separate from Frux's Go four-layer
module convention because it owns no Frux domain facts or persistence.

Alternative considered: embed Python or ONNX inference in the Go worker. Rejected because this proposal must not integrate with the worker, and it would mix model/runtime lifecycle with queue processing.

Alternative considered: a general-purpose model server. Rejected because model selection and broad serving features create unnecessary attack surface and an unstable downstream contract.

### 2. Pin one multilingual MiniLM contract

The initial model is:

- model: `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2`
- Hugging Face revision: `e8f8c211226b894fcb81acc59f3b34ba3efd5f42`
- license: Apache-2.0
- output dimension: 384
- maximum sequence length: 128 tokens
- output dtype: `float32`
- pooling: model-packaged mean pooling
- post-processing: L2 normalization
- device: CPU

This model is small enough for bounded CPU deployment, supports Chinese and other Frux-relevant languages, and has a widely used Sentence Transformers contract. The immutable revision is downloaded during image build into `/opt/frux/models/paraphrase-multilingual-MiniLM-L12-v2`, owned by root and read-only at runtime. `HF_HUB_OFFLINE=1`, `TRANSFORMERS_OFFLINE=1`, and local-files-only loading prevent runtime downloads. Constants in the app, not environment or request fields, define model identity and behavior.

At startup, the loader verifies the expected model/revision metadata and a committed Chinese fixture vector before readiness. This catches accidental artifact, dependency, pooling, or normalization drift even when filenames still exist.

Alternative considered: `multilingual-e5-small`. Rejected for the initial contract because it requires query/document prefix semantics that are unnecessary for one title/description document encoder and easier for later callers to misuse.

Alternative considered: an ONNX quantized artifact. Rejected initially because lower memory would come with a second conversion/quantization contract. The first version prioritizes a direct pinned upstream Sentence Transformers artifact and deterministic fixtures; ONNX can be a separately versioned future model.

### 3. Use a minimal versioned HTTP contract

Endpoints:

| Method | Path | Authentication | Response |
| --- | --- | --- | --- |
| `GET` | `/health/live` | none | process-only `{"status":"live"}` |
| `GET` | `/health/ready` | none | `200 ready` only after preload/self-check, otherwise `503 not_ready` |
| `GET` | `/internal/v1/model` | `X-Internal-Token` | exact model metadata and fixed limits |
| `POST` | `/internal/v1/embeddings` | `X-Internal-Token` | exact model metadata plus ordered item vectors |

The app registers no documentation UI, CORS middleware, cookies, static files, metrics endpoint, or administrative/model-loading route. Default Compose uses `expose: 8081` without `ports`, so only the Compose network can reach it. Health endpoints reveal only process state and are unauthenticated so orchestration can probe without placing the shared token in command arguments. Metadata and inference always require the internal token.

Embedding request:

```json
{
  "items": [
    {
      "id": "video:1001",
      "title": "城市夜景",
      "description": "雨后的街道与霓虹灯"
    }
  ]
}
```

Successful response:

```json
{
  "model": "sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2",
  "revision": "e8f8c211226b894fcb81acc59f3b34ba3efd5f42",
  "dimension": 384,
  "items": [
    {
      "id": "video:1001",
      "index": 0,
      "embedding": [0.0123]
    }
  ]
}
```

The example vector is abbreviated only in documentation; runtime responses require exactly 384 components. Metadata additionally returns `max_sequence_tokens`, `dtype`, `normalized`, `device`, and request limits.

Alternative considered: return embeddings as base64 or a binary tensor format. Rejected because small bounded JSON batches simplify the first internal contract and tests; the maximum response remains bounded by 32 × 384 floats.

### 4. Normalize and bound text before tokenization

The JSON decoder rejects malformed JSON, trailing content, unknown fields, missing fields, and duplicate IDs. The ASGI receive path counts bytes and stops at 131,072 bytes before normal parsing. A batch contains 1–32 items.

Each item has:

- `id`: 1–128 ASCII characters matching `[A-Za-z0-9][A-Za-z0-9._:-]*`, unique in the batch;
- `title`: required, 1–200 Unicode code points after normalization;
- `description`: required but may normalize to empty, 0–2,000 code points;
- no other fields.

Title and description are independently normalized with Unicode NFKC, edge-trimmed, and all Unicode whitespace runs collapsed to one ASCII space. Remaining Unicode control and surrogate categories are rejected. The model input is the title alone when description is empty, otherwise `title + "\n" + description`. Aggregate normalized title-plus-description content is capped at 16,384 code points per request. Limits are evaluated after normalization to prevent compatibility characters or whitespace from bypassing the contract.

The service does not interpret URLs, tokens, markup, or language; they remain ordinary bounded text and are never logged or persisted. The model's fixed 128-token truncation remains part of metadata, while code-point bounds protect decoding and tokenization work.

Alternative considered: accept one arbitrary `text` field. Rejected because preserving title/description structure now prevents every future caller from inventing an incompatible concatenation rule.

### 5. Make inference ordered and deterministic

At startup, one 180-second monotonic outer deadline covers the killable preload process, packaged
model/metadata/fixture validation, and initialization of every configured inference child. Each fixed
inference child repeats the same immutable preload before accepting work, sets deterministic CPU
seeds/settings, enables evaluation and no-gradient/inference mode, disables tokenizer parallelism,
and fixes PyTorch/BLAS/OpenMP threads to two. A request is split into consecutive chunks of 8 and
encoded sequentially with `normalize_embeddings=True`, without dynamic batching across requests.
Results are converted to `float32`, checked for shape `(n, 384)`, finiteness, and unit norm, then
reassembled in original order.

Every output includes the caller ID and zero-based request index. Any item failure fails the complete request; partial vectors are never returned. Committed Chinese and mixed-language fixture vectors compare all 384 components at `atol=1e-6`, `rtol=1e-5`. Tests also compare repeated, single-item, multi-item, and chunk-boundary calls.

Alternative considered: cross-request dynamic batching for throughput. Rejected because it complicates deadlines, fairness, deterministic behavior, and backpressure for a small initial CPU service.

### 6. Match Frux service authentication and secret handling

`FRUX_INTERNAL_TOKEN` is the only credential. Startup trims and validates it using the same Frux policy: at least 32 characters, not the known placeholder, and at least three of lowercase, uppercase, digit, and other classes. Protected requests trim `X-Internal-Token` and use `hmac.compare_digest` against the configured value. Missing and mismatched credentials use the existing stable codes `AUTH_INTERNAL_TOKEN_REQUIRED` and `AUTH_INVALID_INTERNAL_TOKEN`.

JWTs, cookies, query parameters, and JSON fields are not credentials. The default Compose service receives the same secret as API/worker but has no browser route or host port. Authentication executes before body parsing or capacity admission so unauthorized requests cannot consume tokenizer/model work.

Logs, exceptions, validation details, and test snapshots must never contain either token. Timing tests will not attempt to prove constant-time behavior statistically; unit tests will verify the comparison path uses `hmac.compare_digest`, while HTTP tests cover all auth outcomes.

Alternative considered: a new embedding-specific token. Rejected for this first internal service because the requirement is consistency with current Frux internal-token practice. Secret separation can be introduced later as a coordinated security change.

### 7. Apply explicit capacity, timeout, and backpressure limits

The runtime uses one HTTP coordinator, two inference child processes/slots, and a bounded admission
counter for eight waiting requests. Authentication and input validation occur before queue
admission. If the admitted waiting capacity is full, the service returns `429 OVER_CAPACITY` and
`Retry-After: 1`. An admitted request has at most 2 seconds to acquire an inference slot. The
complete handler, including validation, queue time, inference, vector validation, and serialization,
has a 15-second deadline; timeout returns no partial result, terminates the executing child, releases
admission immediately, and asynchronously replaces the slot with a freshly preloaded process.
Replacement preload failures retry with bounded 100 ms, 500 ms, 1 s, 2 s, then capped 5 s backoff
until shutdown. The pool reports currently live workers; readiness requires the full configured
capacity and returns to ready only after all missing replacements have successfully preloaded.

Supported configuration:

| Variable | Required/default | Validation |
| --- | --- | --- |
| `FRUX_INTERNAL_TOKEN` | required | existing strong-token policy |
| `FRUX_EMBEDDING_BIND_HOST` | `0.0.0.0` | IP literal only |
| `FRUX_EMBEDDING_PORT` | `8081` | 1–65535 |
| `FRUX_EMBEDDING_MAX_CONCURRENCY` | `2` | integer 1–2 |
| `FRUX_EMBEDDING_MAX_QUEUE` | `8` | integer 0–8 |
| `FRUX_EMBEDDING_QUEUE_TIMEOUT_MS` | `2000` | integer 100–2000 |
| `FRUX_EMBEDDING_REQUEST_TIMEOUT_MS` | `15000` | integer 1000–15000 and greater than queue timeout |
| `FRUX_EMBEDDING_LOG_LEVEL` | `INFO` | `WARNING`, `INFO`, or `DEBUG` |

Any unknown `FRUX_EMBEDDING_*` variable fails startup so misspellings cannot silently remove bounds. `OMP_NUM_THREADS=2`, `MKL_NUM_THREADS=2`, `OPENBLAS_NUM_THREADS=2`, `NUMEXPR_NUM_THREADS=2`, and `TOKENIZERS_PARALLELISM=false` are fixed by the image/Compose contract and checked on startup. Uvicorn workers are fixed at one rather than configurable.

Compose applies a 2-CPU/2-GiB limit and documents a 1-CPU/1-GiB minimum reservation. Operators needing more throughput scale replicas horizontally behind an internal load balancer rather than increasing per-process bounds.

The coordinator does not rely on Python thread cancellation for native kernels. Every inference runs
in a dedicated child process; an end-to-end timeout or request cancellation terminates that child,
discards all output, and starts a replacement. No timed-out native call retains a slot or survives as
an untracked background process.

Alternative considered: an unbounded executor queue with only an HTTP timeout. Rejected because timed-out work would continue accumulating and exhaust memory/CPU.

### 8. Fail closed on startup and return safe bounded errors

Startup has one 180-second outer timeout covering settings validation, killable model preload,
metadata/fixture checks, and complete inference-pool initialization. Workers consume only the
remaining outer budget; they do not each receive an independent 180-second timeout. Any failure
terminates startup children, logs a stable startup result class, and exits non-zero; it does not
start degraded with a substitute model. Inference-process recycling reloads only the same packaged
immutable model contract.

Errors use the Frux envelope:

```json
{"code":"INVALID_REQUEST","error":"invalid request"}
```

Stable categories include authentication errors, `INVALID_JSON`, `INVALID_REQUEST`, `REQUEST_TOO_LARGE`, `OVER_CAPACITY`, `INFERENCE_TIMEOUT`, `NOT_READY`, and `INTERNAL_ERROR`. Messages are generic and do not echo input or infrastructure details. Custom exception handlers replace framework validation/trace output with this envelope and bound all response bodies.

Operational request logs contain only `route`, `status`, `duration_ms`, bounded `result`, and current
live `capacity`. Route and result values come from closed registries. Logs exclude headers, bodies,
normalized text, item IDs, vectors, tokens, raw paths, URLs, model filesystem paths, cache URLs, and
raw exception text. Uvicorn access logging remains disabled. The service has no database, queue,
cache, request-history file, or analytics sink.

### 9. Package the model in a hardened Compose service

The multi-stage image resolves locked Python dependencies and downloads the model revision during build. The runtime stage copies only the virtual environment, app, fixtures, and model snapshot. It runs as a numeric non-root user, sets the root filesystem read-only in Compose, sets `TMPDIR=/run/frux-tmp`, mounts only that path as a small `tmpfs` with size/noexec/nosuid/nodev options, drops all Linux capabilities, and uses `no-new-privileges`.

The Compose service is named `semantic-embedding`, has no dependency on PostgreSQL, Redis, RabbitMQ, API, worker, or Web, and publishes no host port. Its readiness healthcheck uses Python's standard library against `127.0.0.1:8081/health/ready`, avoiding an extra curl dependency. Health timing allows up to the 180-second preload window. The model directory remains read-only.

Service documentation will be `docs/modules/semantic-embedding.md`; `docs/modules/README.md`, `docs/engineering.md`, `docs/architecture.md`, `docs/deployment.md`, and the relevant README/configuration examples will be updated during implementation because this adds a new deployable module. Those documentation updates describe the standalone boundary and must not claim Go integration.

### 10. Verify behavior at unit, contract, image, and Compose levels

Pytest coverage will include:

- settings and strong-token validation;
- NFKC/whitespace/control normalization and all exact/over-limit boundaries;
- strict schema, malformed/trailing JSON, body cap, duplicate IDs, and unknown fields;
- auth before body/inference, using the constant-time helper;
- exact metadata and response schema;
- real-model deterministic Chinese/mixed fixtures, dimension, dtype, finiteness, norm, repeatability, identity/order, and chunk boundaries;
- startup fixture failure and readiness behavior;
- capacity admission, queue timeout, request timeout, release after cancellation/error, and no partial output;
- safe error envelopes and log redaction.

Container contract tests will start the built image with network disabled after construction, verify non-root/offline behavior, inspect metadata, and run real embeddings. Compose validation will verify no host port, resource/security settings, and a healthy service. The implementation gate is the locked pytest suite, image build and image contract suite, `docker compose config`, and `openspec validate --all --strict`.

## Risks / Trade-offs

- [The model image and Python/PyTorch dependencies are large] → Keep the service isolated, use a multi-stage build, package only the pinned snapshot/runtime, and document the image/resource cost.
- [CPU floating-point output can vary slightly across supported hosts] → Pin runtime dependencies and thread settings, use full-vector fixtures with tight tolerances rather than byte equality, and version any future runtime/model change.
- [The 128-token model limit can truncate long descriptions] → Expose the limit in metadata, keep deterministic title-first composition, and treat a different model/sequence length as a new contract.
- [A native inference call can outlive an HTTP timeout] → Run it only in a killable child process, terminate/recycle that process at the deadline, release admission, and verify no process or input-bearing log leaks.
- [Two concurrent inferences can approach the 2-GiB limit on some CPUs] → Add an image smoke/load test under the stated limit, permit reducing concurrency to one, and scale horizontally instead of raising limits.
- [Sharing `FRUX_INTERNAL_TOKEN` broadens the effect of that secret] → Keep the service network-internal, never log it, validate strength, and leave per-service credentials to a coordinated future change.
- [Unauthenticated health endpoints reveal process state] → Return status only, expose no host port, and keep all metadata/inference protected.

## Migration Plan

1. Add `apps/semantic-embedding` with locked Python environment, pinned model build, source, fixtures, and tests.
2. Build and run the image independently with runtime network disabled; verify startup fixture, metadata, deterministic vectors, auth, limits, and non-root filesystem behavior.
3. Add the internal-only `semantic-embedding` service to Compose with the shared secret, healthcheck, offline/thread settings, and resource/security bounds.
4. Update module, engineering, architecture, deployment, and root documentation to describe operation and explicitly state that no Frux caller consumes the service yet.
5. Validate the locked test suite, image contract tests, `docker compose config`, and strict OpenSpec validation.
6. Rollback by removing/stopping only the semantic embedding container. Existing API, worker, Web, PostgreSQL, Redis, RabbitMQ, and recommendation behavior require no data or code rollback.

## Open Questions

None. Model identity, vector dimension, input limits, resource bounds, API shape, authentication, deployment isolation, and deferred integration are fixed by this proposal.
