## ADDED Requirements

### Requirement: Standalone Reproducible Embedding Runtime
Frux SHALL provide the semantic embedding capability as a separately deployable CPU-only Python service under its own application directory. The service SHALL use Python 3.12, a committed lockfile with exact transitive dependency versions and hashes, a digest-pinned container base, and model artifacts fetched at image build time from an immutable revision. Runtime startup and requests MUST operate with model-hub network access disabled.

#### Scenario: Image is built reproducibly
- **WHEN** the service image is built from the committed application directory and lockfile
- **THEN** it installs only locked dependencies and packages the model files from the specified immutable revision

#### Scenario: Runtime has no model network access
- **WHEN** a container starts without access to Hugging Face or another model registry
- **THEN** it loads the packaged model and can become ready without downloading any file

#### Scenario: Packaged model is missing
- **WHEN** required model artifacts or their expected metadata are absent or inconsistent
- **THEN** startup fails non-zero and the service never reports readiness

### Requirement: Fixed Initial Model Contract
The service SHALL expose only `sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2` at revision `e8f8c211226b894fcb81acc59f3b34ba3efd5f42`. It SHALL use the model's 128-token maximum sequence length and return 384-dimensional, L2-normalized, finite `float32` embeddings. Neither configuration nor a request MAY select another model, revision, dimension, pooling strategy, normalization mode, or device.

#### Scenario: Caller reads model metadata
- **WHEN** an authenticated caller requests model metadata
- **THEN** the response identifies the exact model, revision, dimension 384, maximum sequence length 128, `float32` dtype, L2 normalization, and CPU runtime

#### Scenario: Caller attempts model selection
- **WHEN** an embedding request contains a model, revision, device, dimension, pooling, or normalization field
- **THEN** strict request validation rejects the unknown field without loading or selecting another model

#### Scenario: Model produces an invalid vector
- **WHEN** inference returns a vector with the wrong dimension, a non-finite component, or a non-unit norm outside the documented tolerance
- **THEN** the request fails with a safe internal error and no partial embeddings are returned

### Requirement: Minimal Internal HTTP Surface
The service SHALL expose only `GET /health/live`, `GET /health/ready`, `GET /internal/v1/model`, and `POST /internal/v1/embeddings`. Health responses SHALL contain status only. The model and embedding endpoints SHALL be internal service-to-service APIs, SHALL NOT enable CORS or browser credentials, and SHALL NOT be published on a host port by the default Compose configuration.

#### Scenario: Process is live and model is ready
- **WHEN** the process is running, startup model validation has completed, and every configured inference worker is live
- **THEN** liveness returns `200` with `{"status":"live"}` and readiness returns `200` with `{"status":"ready"}`

#### Scenario: Model is not ready
- **WHEN** readiness is checked before successful model preload and self-validation
- **THEN** readiness returns `503` with `{"status":"not_ready"}` and exposes no loading details

#### Scenario: Unsupported route or method is requested
- **WHEN** a caller requests any other path or an unsupported method
- **THEN** the service returns a bounded safe `404` or `405` response and performs no inference

### Requirement: Frux Internal Token Authentication
Every model-metadata and embedding request SHALL require `X-Internal-Token` and compare its trimmed value with `FRUX_INTERNAL_TOKEN` using a constant-time comparison. Startup SHALL reject a missing token, `replace-with-internal-token`, values shorter than 32 characters, or values containing fewer than three of lowercase, uppercase, digit, and other character classes. Browser JWTs, cookies, query parameters, and request bodies MUST NOT act as service credentials.

#### Scenario: Valid internal token is supplied
- **WHEN** a caller supplies the configured strong token in `X-Internal-Token`
- **THEN** the request proceeds to endpoint validation

#### Scenario: Internal token is missing or invalid
- **WHEN** the header is absent or does not match
- **THEN** the service returns `401` with `AUTH_INTERNAL_TOKEN_REQUIRED` or `AUTH_INVALID_INTERNAL_TOKEN` and performs no model inference

#### Scenario: Internal token contains non-ASCII bytes
- **WHEN** configuration or a request header contains a non-ASCII or control character
- **THEN** configuration fails or the request returns bounded `401 AUTH_INVALID_INTERNAL_TOKEN` before constant-time comparison, without a `TypeError` or 500

#### Scenario: Weak token is configured
- **WHEN** the process starts with a missing, placeholder, short, or insufficiently varied `FRUX_INTERNAL_TOKEN`
- **THEN** startup fails non-zero before the service accepts traffic

### Requirement: Strict Normalized Batch Input
`POST /internal/v1/embeddings` SHALL accept a strict JSON object containing only `items`. Each item SHALL contain exactly `id`, `title`, and `description`; IDs SHALL be unique within the batch, 1 to 128 ASCII characters, and match `[A-Za-z0-9][A-Za-z0-9._:-]*`. The service SHALL normalize title and description independently using Unicode NFKC, trim edges, and collapse each run of Unicode whitespace to one ASCII space. It SHALL reject remaining control or surrogate code points. The normalized title SHALL contain 1 to 200 Unicode code points, the normalized description SHALL contain 0 to 2,000, the batch SHALL contain 1 to 32 items, the total normalized title-plus-description count SHALL not exceed 16,384 code points, and the HTTP body SHALL not exceed 131,072 bytes.

#### Scenario: Valid Chinese title and description are submitted
- **WHEN** a request contains one or more bounded items with Chinese or multilingual text
- **THEN** the service normalizes each field, combines non-empty fields as `title + "\\n" + description`, and embeds the items in request order

#### Scenario: Equivalent Unicode and whitespace are submitted
- **WHEN** canonically compatible text differs only by NFKC form or runs of Unicode whitespace
- **THEN** it produces the same normalized model input and embedding

#### Scenario: Item or aggregate limit is exceeded
- **WHEN** the body, batch count, identity, title, description, or aggregate normalized code-point limit is exceeded
- **THEN** the complete request is rejected with `400` or `413` before inference and no partial result is returned

#### Scenario: JSON shape is not exact
- **WHEN** JSON is malformed, has trailing content, omits a required field, contains a duplicate item ID, or includes an unknown field
- **THEN** the complete request is rejected with a stable safe validation error

### Requirement: Ordered Deterministic Embedding Response
A successful embedding response SHALL include the fixed model, revision, dimension, and an `items` array. Every output item SHALL contain the input `id`, its zero-based `index`, and one 384-component embedding. Item identity and order SHALL exactly match the request. Inference SHALL use evaluation/no-gradient mode, fixed deterministic runtime settings, and fixed internal chunks of 8 items without cross-request dynamic batching.

#### Scenario: Batch embedding succeeds
- **WHEN** a valid authenticated batch is processed
- **THEN** the service returns `200`, exactly one output per input, the same IDs and order, indexes `0..n-1`, and finite unit-normalized vectors

#### Scenario: Same normalized input is repeated
- **WHEN** the same normalized text is embedded repeatedly or appears in single-item and multi-item requests
- **THEN** all 384 components match within absolute tolerance `1e-6` and relative tolerance `1e-5`

#### Scenario: Internal chunk boundary is crossed
- **WHEN** a batch contains more than 8 items
- **THEN** fixed sequential chunking preserves the same vectors, identities, and order as processing each item separately

### Requirement: Preload and Startup Self-Validation
The service SHALL use exactly one HTTP server coordinator and at most two isolated inference worker
processes. A killable preload process SHALL fully load the packaged model and run the deterministic
Chinese fixture before readiness. Every inference worker SHALL preload the same immutable packaged
model contract before receiving work. One 180-second outer deadline SHALL cover model preload,
metadata and fixture validation, and initialization of the full configured inference pool; workers
MUST consume the remaining outer budget rather than receive independent 180-second deadlines.

#### Scenario: Startup self-check succeeds
- **WHEN** the packaged model matches the pinned contract and produces the expected fixture vector
- **THEN** startup completes and readiness becomes successful

#### Scenario: Startup times out or self-check differs
- **WHEN** preload, fixture validation, or complete pool initialization exceeds the single 180-second deadline, or the fixture result falls outside tolerance
- **THEN** the process exits non-zero and orchestration keeps it out of service

#### Scenario: Request arrives after readiness
- **WHEN** an authenticated embedding request is accepted
- **THEN** it uses the already resident model and performs no model load, download, replacement, or warm-up mutation

### Requirement: Bounded CPU, Memory, Concurrency, and Time
The default deployment SHALL run one HTTP coordinator with two killable inference worker processes,
at most eight admitted waiting requests, two CPU threads per inference process, a 15-second
end-to-end request deadline, and a 2-second maximum queue wait. Configuration MAY reduce these
bounds but MUST NOT exceed them. The Compose service SHALL apply a 2-CPU and 2-GiB memory limit
and SHALL document 1 CPU and 1 GiB as the minimum reservation guidance.

#### Scenario: Capacity is available
- **WHEN** a request obtains an inference slot within 2 seconds and completes within 15 seconds
- **THEN** it returns the complete embedding response

#### Scenario: Waiting queue is full
- **WHEN** two requests are inferring and eight requests are already admitted to wait
- **THEN** another request receives `429 OVER_CAPACITY` with `Retry-After: 1` without entering inference

#### Scenario: Queue or inference deadline expires
- **WHEN** slot acquisition exceeds 2 seconds or total processing exceeds 15 seconds
- **THEN** the request returns a safe `429` or `504` response, terminates and replaces any executing inference process, releases admission, and returns no partial vectors

#### Scenario: Native inference does not return
- **WHEN** a model/native kernel remains blocked past the end-to-end deadline
- **THEN** the coordinator kills that isolated process, makes the old PID ineligible for reuse, restores the slot with a freshly preloaded process, and leaves no live orphan process

#### Scenario: Client disconnects during inference
- **WHEN** ASGI reports `http.disconnect` while a child process is executing inference
- **THEN** the coordinator cancels and recycles that child immediately, releases admission/capacity, returns no vector, and a subsequent request can proceed

#### Scenario: Replacement preload fails
- **WHEN** one or all inference workers are lost and replacement preload temporarily fails
- **THEN** replacement retries continue with bounded capped backoff, readiness returns `503` while live capacity is below the configured requirement, and readiness recovers after all missing workers are restored

#### Scenario: Shutdown occurs during replacement
- **WHEN** shutdown begins while replacement workers are sleeping or preloading
- **THEN** retries stop, starting and live children are terminated, and shutdown leaves no inference child alive

#### Scenario: Invalid runtime bound is configured
- **WHEN** concurrency, queue, thread, or timeout configuration exceeds its allowed maximum or is non-positive
- **THEN** startup fails non-zero rather than silently accepting an unbounded value

### Requirement: Safe Errors and Privacy-Bounded Operation
All JSON errors SHALL use `{"code":"<STABLE_CODE>","error":"<safe text>"}` with bounded stable codes and generic text. Responses and logs MUST NOT expose stack traces, filesystem paths, dependency errors, model cache URLs, secrets, request bodies, normalized text, item IDs, vectors, raw paths, URLs, or raw errors. The service SHALL keep no request history and SHALL write no application data. Request logs SHALL contain only closed-registry `route`, numeric `status`, numeric `duration_ms`, bounded `result`, and numeric live `capacity`; Uvicorn raw access logging MUST remain disabled.

#### Scenario: Unexpected inference failure occurs
- **WHEN** the model runtime raises an unexpected exception
- **THEN** the caller receives `500 INTERNAL_ERROR` without implementation details and logs contain only the bounded result class and timing

#### Scenario: Token or URL appears in input
- **WHEN** a request body contains text resembling a token or URL
- **THEN** the service processes it only as bounded text and neither logs nor persists the raw value

#### Scenario: Authentication fails
- **WHEN** an invalid token is presented
- **THEN** neither the supplied token nor a derived token value appears in the response, logs, metrics, or model input

### Requirement: Container, Compose, Configuration, and Healthcheck Contract
The implementation SHALL provide a non-root container image, read-only packaged model artifacts, an unprivileged runtime filesystem with only a bounded temporary directory, and a Compose service named `semantic-embedding`. Compose SHALL pass the existing `FRUX_INTERNAL_TOKEN`, set offline and CPU-thread controls, expose only the container-internal service port, apply the resource bounds, and use `GET /health/ready` for a healthcheck. The service SHALL document every supported environment variable and reject unknown `FRUX_EMBEDDING_*` variables.

#### Scenario: Compose configuration is rendered
- **WHEN** `docker compose config` runs with a strong `FRUX_INTERNAL_TOKEN`
- **THEN** the semantic embedding service has a build context, internal-only port, healthcheck, offline settings, single worker, and declared CPU/memory bounds

#### Scenario: Healthcheck runs after startup
- **WHEN** the model has preloaded and passed self-validation
- **THEN** the container healthcheck succeeds without requiring a browser-visible route or external dependency

#### Scenario: Unsupported service configuration is supplied
- **WHEN** an unknown `FRUX_EMBEDDING_*` variable or an invalid supported value is present
- **THEN** startup fails with a safe configuration error

### Requirement: Verification and Service Documentation
The service SHALL include unit and HTTP contract tests that use the real packaged pinned model where vector behavior is asserted. Tests SHALL cover normalization, all size/count/code-point bounds, strict JSON, exact metadata, authentication, deterministic fixture vectors, finite unit norms, identity/order preservation, chunk boundaries, overload, timeout, readiness, and safe errors. Documentation SHALL describe the API, fixed model contract, environment, container/Compose operation, healthcheck, resource guidance, security boundary, and deferred integrations.

#### Scenario: Deterministic vector contract is tested
- **WHEN** the pinned environment runs the committed Chinese and mixed-language fixtures
- **THEN** every component matches committed expected 384-dimensional vectors within absolute tolerance `1e-6` and relative tolerance `1e-5`

#### Scenario: Invalid input and authentication suite runs
- **WHEN** contract tests exercise missing/wrong tokens, malformed and unknown JSON, duplicates, every boundary plus one, overload, and timeout
- **THEN** each request returns the documented status/code and no inference occurs for pre-inference failures

#### Scenario: Implementation validation runs
- **WHEN** implementation is complete
- **THEN** locked-environment tests, image build, image contract tests, `docker compose config`, and `openspec validate --all --strict` succeed

### Requirement: No Recommendation or Persistence Integration
This change SHALL NOT call or modify the Go API or recommendation worker, consume or publish RabbitMQ messages, access PostgreSQL or Redis, persist or backfill video embeddings, add vector columns or ANN indexes, change recommendation policies, or train a recommendation model. A later `integrate-semantic-video-embeddings` change SHALL own consumption and persistence integration.

#### Scenario: Standalone service is deployed
- **WHEN** the semantic embedding container runs
- **THEN** its only runtime dependencies are its packaged model, CPU/memory, configuration, and authenticated HTTP callers

#### Scenario: Existing Frux components run without the service
- **WHEN** the Go API, worker, or Web application starts while the embedding service is absent
- **THEN** their current startup and behavior remain unchanged
