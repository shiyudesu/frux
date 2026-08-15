## 1. Recommendation Roadmap Gate

- [ ] 1.1 Verify `persist-recommendation-training-impressions` is implemented, reviewed, archived, and its trusted-impression acceptance gate is met.
- [ ] 1.2 Verify `export-recommendation-training-dataset` is implemented, reviewed, archived, and deterministic/privacy acceptance gates are met.
- [ ] 1.3 Verify `evaluate-recommendation-policies-offline` is implemented, reviewed, archived, and observational-evaluation acceptance gates are met.
- [ ] 1.4 Verify `learn-recommendation-policy-weights` is implemented, reviewed, archived, and produces only disabled candidate policies.
- [ ] 1.5 Add a documented preflight proving this semantic-service change cannot be applied before all four prerequisites.

## 2. Standalone Runtime and Fixed Model

- [ ] 2.1 Create an independent `apps/semantic-embedding` Python 3.12 app with no dependency on `apps/api` or `apps/web`.
- [ ] 2.2 Add a hash-locked dependency file and digest-pinned multi-stage image.
- [ ] 2.3 Fix model name, immutable revision, dimension 384, sequence length 128, dtype, normalization, CPU/thread controls, and request bounds as constants.
- [ ] 2.4 Download and verify only the fixed model revision at image build time; prohibit runtime model downloads and selection.
- [ ] 2.5 Implement killable process-isolated inference workers that receive only immutable model/fixture paths, never a loaded Torch/model object.
- [ ] 2.6 Apply one 180-second deadline across preload, fixture validation, and complete pool initialization with sanitized failure categories.

## 3. Internal HTTP Contract and Security

- [ ] 3.1 Expose only liveness, readiness, fixed model metadata, and bounded batch embedding routes; disable docs, CORS, cookies, redirects, and host exposure.
- [ ] 3.2 Authenticate protected routes before body parsing with the shared printable-ASCII strong internal-token contract and constant-time comparison.
- [ ] 3.3 Enforce strict body, batch, item, ID, title, description, aggregate-codepoint, and exact JSON-shape bounds.
- [ ] 3.4 Implement exact NFKC/Unicode-whitespace/control normalization and deterministic `title` or `title + \"\\n\" + description` composition.
- [ ] 3.5 Return only bounded stable error envelopes without paths, raw exceptions, dependency details, tokens, text, IDs, vectors, or URLs.

## 4. Deterministic Bounded Inference

- [ ] 4.1 Implement fixed sequential chunks of eight, ordered identity-preserving responses, finite 384-component `float32` vectors, and final unit-normalization validation.
- [ ] 4.2 Reserve two active and eight waiting request slots before body parsing and configure a bounded Uvicorn connection limit.
- [ ] 4.3 Apply one 15-second deadline to receive, parse, authenticate, queue, infer, and send; apply a two-second queue wait.
- [ ] 4.4 Terminate and replace hung/disconnected inference workers, release capacity, and recover readiness only after full live capacity returns.
- [ ] 4.5 Emit only bounded route/status/duration/result/capacity operational logs with raw Uvicorn access logs disabled.

## 5. Tests and Fixtures

- [ ] 5.1 Commit Chinese and multilingual 384-component deterministic fixtures for the exact model revision and tolerances.
- [ ] 5.2 Add settings, token, normalization, schema, boundary, authentication-before-parse, safe-error, and log-redaction tests.
- [ ] 5.3 Add real-model metadata/vector/order/repeatability/chunk/finite/norm fixture tests.
- [ ] 5.4 Add concurrency, queue, overload, slow-upload, stalled-send, timeout, disconnect, replacement, readiness-loss/recovery, and shutdown tests.
- [ ] 5.5 Add startup tests for missing/corrupt model, metadata mismatch, fixture mismatch, timeout, spawn/bootstrap failure, and bind failure without traceback leakage.

## 6. Independent Compose Service

- [ ] 6.1 Add an internal-only `semantic-embedding` Compose service with shared token, readiness healthcheck, read-only root/model, bounded tmpfs, non-root user, dropped capabilities, and CPU/memory limits.
- [ ] 6.2 Keep API and Worker free of semantic configuration, dependency, health gate, Go client, database table, queue, or service call.
- [ ] 6.3 Add image and Compose contract tests for offline startup, immutable artifacts, non-root/read-only execution, internal-only exposure, resource limits, and forbidden dependencies.

## 7. Documentation and Validation

- [ ] 7.1 Add semantic-service module documentation and update architecture, engineering, deployment, module index, and setup docs.
- [ ] 7.2 Explicitly document that the capability is roadmap step 5, is not part of Kafka migration, and has no Go caller until `integrate-semantic-video-embeddings`.
- [ ] 7.3 Run the frozen Python suites, live pinned-model fixtures, image build/offline contract, Compose service health/contract, and security/resource tests.
- [ ] 7.4 Confirm no Go API/Worker/Web, PostgreSQL, Redis, Kafka, persisted embedding, backfill, profile, pgvector, ANN, policy, or training changes are introduced.
- [ ] 7.5 Run `docker compose config` and `openspec validate --all --strict`.
