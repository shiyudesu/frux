## 1. Standalone Runtime and Fixed Model

- [x] 1.1 Create the independent `apps/semantic-embedding` Python 3.12 application with `src/frux_embedding`, tests, deterministic fixture storage, package metadata, and no dependency on `apps/api` or `apps/web`.
- [x] 1.2 Define the FastAPI/Uvicorn/Sentence Transformers/PyTorch CPU dependencies, commit a hash-bearing `uv.lock` with exact transitive versions, and verify frozen installation succeeds.
- [x] 1.3 Implement immutable constants and validated settings for model `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2`, revision `e8f8c211226b894fcb81acc59f3b34ba3efd5f42`, dimension 384, sequence length 128, `float32`, L2 normalization, chunk size 8, request bounds, one worker, two threads, the strong `FRUX_INTERNAL_TOKEN` policy, and rejection of unknown or invalid `FRUX_EMBEDDING_*` values.
- [x] 1.4 Add build-time download and metadata verification for only the fixed model revision, then implement local-files-only CPU preload with deterministic seeds, evaluation/inference mode, fixed thread controls, tokenizer parallelism disabled, and no runtime download, reload, or model-selection path.

## 2. Lifecycle, Fixtures, and HTTP Contract

- [x] 2.1 Generate and commit full 384-component Chinese and mixed-language fixture vectors with exact model/revision and tolerances, then implement readiness requiring successful preload, metadata validation, and fixture self-check; protected routes reject not-ready state, while missing/corrupt artifacts, mismatches, or a 180-second startup timeout exit non-zero.
- [x] 2.2 Expose exactly `GET /health/live`, `GET /health/ready`, `GET /internal/v1/model`, and `POST /internal/v1/embeddings`, while disabling OpenAPI/docs UI, CORS, cookies, static files, metrics, and model administration routes.
- [x] 2.3 Implement exact model metadata and embedding DTOs containing the fixed model contract and bounded limits, with each response item preserving its original ID, zero-based index, request order, and exactly 384 vector components.

## 3. Authentication, Validation, and Safe Errors

- [x] 3.1 Authenticate protected routes before body parsing or capacity admission by trimming `X-Internal-Token`, comparing with `hmac.compare_digest`, returning the Frux missing/invalid token codes, and never accepting JWTs, cookies, query parameters, or body fields as credentials.
- [x] 3.2 Enforce the 131,072-byte body limit before decoding and strict JSON batches of 1–32 exact `id`/`title`/`description` items, rejecting malformed/trailing JSON, missing or unknown fields, duplicate IDs, invalid 1–128 character ASCII IDs, and aggregate normalized content over 16,384 code points without echoing input.
- [x] 3.3 Normalize title and description independently with NFKC, Unicode-whitespace collapse, and edge trimming; reject controls/surrogates; enforce title 1–200 and description 0–2,000 post-normalization code points; and compose deterministically as `title` or `title + "\n" + description`.
- [x] 3.4 Add bounded custom `404`, `405`, validation, request-size, readiness, overload, timeout, and unexpected-error handlers that return only the stable `{"code","error"}` envelope with no trace, raw exception, path, dependency detail, or input echo.

## 4. Deterministic Inference and Bounded Capacity

- [x] 4.1 Implement sequential chunks of 8 with no cross-request batching, convert outputs to `float32`, and reject any wrong shape, non-finite component, or non-unit vector before returning an all-or-nothing ordered response.
- [x] 4.2 Enforce two true inference slots and at most eight admitted waiters, returning `429 OVER_CAPACITY` with `Retry-After: 1` before enqueueing excess work.
- [x] 4.3 Enforce the 2-second slot wait and 15-second end-to-end deadline, release admission state on every path, return no partial vectors, and terminate/recycle timed-out native inference processes.
- [x] 4.4 Emit only bounded structured operational fields and add guards proving headers, bodies, normalized text, item IDs, vectors, tokens, paths, URLs, and raw exceptions are never logged or persisted; keep the service free of PostgreSQL, Redis, RabbitMQ, Go API, recommendation clients, and request-history storage.

## 5. Unit and Contract Tests

- [x] 5.1 Add settings and authentication tests covering the strong-token policy, valid/missing/wrong/padded and non-header credential cases, every configurable boundary, invalid addresses/enums, fixed worker/thread controls, unknown variables, and proof that rejected auth never parses text, enters the queue, or calls the model.
- [x] 5.2 Add normalization and strict-schema tests covering NFKC/whitespace equivalence, Chinese and multilingual text, controls/surrogates, exact and over-limit title/description/ID/batch/aggregate/body boundaries, duplicate IDs, malformed/trailing JSON, and missing/unknown fields.
- [x] 5.3 Add health, readiness, startup-failure, safe-error, and exact metadata/response tests for not-ready behavior, successful preload, timeout, missing/corrupt model files, metadata mismatch, fixture mismatch, bounded envelopes, and absence of sensitive response or log content.
- [x] 5.4 Add real pinned-model tests for both committed fixtures, all 384 components, dtype conversion, finiteness, unit norm, repeatability, normalized-equivalent input, single/multi-item identity and order, chunk boundaries, and complete failure for any invalid model result.
- [x] 5.5 Add concurrency and timeout tests for two active slots, eight waiters, immediate overflow, queue and total deadlines, hung-process termination/replacement, admission cleanup, no partial responses, no process leaks, and no application-data or request-history files.

## 6. Hardened Image and Compose Service

- [x] 6.1 Add a digest-pinned multi-stage `Dockerfile` with frozen dependencies and immutable build-time model download; copy only runtime files and read-only model artifacts, then run as a numeric non-root user with offline/two-thread controls, one Uvicorn worker, `TMPDIR=/run/frux-tmp`, and no writable model or cache.
- [x] 6.2 Add the internal-only `semantic-embedding` Compose service with the shared strong token, `expose` but no host `ports`, readiness healthcheck and 180-second startup allowance, 2-CPU/2-GiB limits, read-only root filesystem, bounded tmpfs, dropped capabilities, `no-new-privileges`, and no API, worker, database, cache, or queue dependency.
- [x] 6.3 Add image and Compose contract tests covering network-disabled offline startup, non-root identity, read-only filesystems/model, bounded temporary storage, exact metadata, authenticated real embeddings, healthy readiness, security/resource/thread settings, internal-only exposure, and absence of forbidden dependencies.

## 7. Documentation and Final Validation

- [x] 7.1 Add `docs/modules/semantic-embedding.md` and update the module index, engineering, architecture, deployment, and root setup/configuration docs with the exact model/API/environment/health/resource contract, while explicitly deferring browser/storage exposure, Go integration, PostgreSQL/Redis, RabbitMQ, persistence/backfill, pgvector/ANN, recommendation changes, and training.
- [x] 7.2 Run the frozen unit and HTTP suites, including real-model fixture and concurrency/timeout coverage, then build the digest-pinned image and run its offline contract suite under the declared CPU, memory, and filesystem constraints.
- [x] 7.3 Render Compose with a strong test token, run the targeted service health/contract test, confirm existing Go API/worker/Web source and configuration gained no embedding dependency or integration changes, and finish with `openspec validate --all --strict`.

## 8. Review Remediation

- [x] 8.1 Move native model inference and startup self-checks into killable isolated processes.
- [x] 8.2 Terminate and replace a hung inference process at the end-to-end deadline while preserving the two-slot/eight-waiter contract.
- [x] 8.3 Add process replacement, capacity release, no-live-process-leak, and child-output redaction tests; prepare writable runtime tmpfs state for process spawning.
