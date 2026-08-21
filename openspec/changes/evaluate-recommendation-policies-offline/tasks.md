## 1. Common contracts and safe command

- [x] 1.1 Define closed dataset/track/profile/baseline/metric/report versions, bounded identities, availability values, exclusion codes, and `external_model_calls: 0`.
- [x] 1.2 Add strict manifest, relative-path containment, SHA-256, row-count, schema, release, citation, and license-acknowledgement validation with identity-safe errors.
- [x] 1.3 Add `cmd/recommendation-offline-evaluate` subcommand parsing, K/bound/output validation, validation-only plan, and no runtime-service imports.
- [x] 1.4 Add synthetic checked-in fixtures and ignored operator data/report directories without redistributing public rows.

## 2. Public dataset adapters

- [x] 2.1 Implement streaming `kuairec-v2` interaction parsing for documented IDs, durations, timestamp, and watch ratio with consistency tolerance and no synthetic like label.
- [x] 2.2 Implement strict KuaiRec category parsing plus optional canonical author/text/image/multimodal feature channels.
- [x] 2.3 Implement `microlens-canonical-v1` interaction/item/feature parsing bound to manifest release and normalization recipe rather than guessed raw layouts.
- [x] 2.4 Normalize both adapters into isolated dataset-local records and add valid, corrupt, unsupported, duplicate, escaping-path, hash/count, and oversized tests.

## 3. Chronological cases and baseline registry

- [x] 3.1 Implement `short-video-session-v1` positive/quick-skip/neutral classification, chronological ordering, held-out target selection, minimum history, bounded session, prior-item exclusion, and closed case exclusions.
- [x] 3.2 Implement deterministic Popularity and Recent Interaction baselines with fixed final item-key tie-breaks.
- [x] 3.3 Implement Category profile/cosine scoring with deterministic primary-category and missing-category handling.
- [x] 3.4 Implement Text, Image, and Multimodal positive-session centroid Exact cosine baselines using only compatible precomputed vectors.
- [x] 3.5 Implement Multimodal + Session Interest using bounded positive centroid minus negative quick-skip centroid without model inference.
- [x] 3.6 Add leakage, duplicate ordering, target inclusion, neutral/missing behavior, unavailable feature, vector compatibility, and deterministic tie tests.

## 4. Metrics and aggregation

- [x] 4.1 Implement Recall@K, NDCG@K, HitRate@K, reciprocal rank, and paired baseline result aggregation with explicit numerators/denominators.
- [x] 4.2 Implement Catalog Coverage, category/author coverage and concentration, largest-group share, and repeated item/primary-category runs with availability reasons.
- [x] 4.3 Implement per-baseline feature/case coverage, exclusion summaries, dataset counts, and separate dataset result boundaries.
- [x] 4.4 Implement deterministic ranking work summaries and checksum-covered optional Exact-latency/upstream embedding-throughput evidence.
- [x] 4.5 Add metric edge-case, K-bound, empty/unavailable, repeated-run, and exact expected-report tests.

## 5. Production replay and Golden Sets

- [x] 5.1 Export the production policy normalized-clone validator and prove `NewPolicy` retains identical valid/invalid behavior.
- [x] 5.2 Implement strict named policy decoding, replayable-difference classification, diagnostic-only suppression, and canonical configuration hashes.
- [x] 5.3 Implement fixed-feature-order scorer replay, production tie-breaking/diversity, exact canonical parity, and served-subset limitations.
- [x] 5.4 Implement versioned Query/Similar/Session Direction/Negative Suppression Golden cases, blinded 0-3 annotations, adjudication, agreement, semantic NDCG, direction, and suppression metrics.
- [x] 5.5 Add production parity, non-replayable rejection, diagnostic-only, annotation provenance, incomplete adjudication, and cross-track label rejection tests.

## 6. Reports and orchestration

- [x] 6.1 Define canonical public/replay/golden report contracts with provenance, hashes/counts, profiles, availability, metrics, exclusions, latency, limitations, and no raw/vector/path leakage.
- [x] 6.2 Implement deterministic JSON and Markdown rendering from one report value with no wall-clock-dependent fields.
- [x] 6.3 Implement paired `0600` sibling partial files, sync, atomic publication, safe overwrite, and failure cleanup that preserves existing outputs.
- [x] 6.4 Wire all three CLI tracks and add command/filesystem/end-to-end tests proving no DB/Redis/Kafka/HTTP/S3/provider dependency or model call.

## 7. Documentation and verification

- [x] 7.1 Document operator-owned MicroLens/KuaiRec preparation, source/license acknowledgement, manifests, feature provenance, commands, thresholds, baselines, and metrics.
- [x] 7.2 Document dataset isolation, public-vs-Golden-vs-replay evidence boundaries, no causal/promotion claim, and deferred training/Shadow gates.
- [x] 7.3 Update the roadmap status and add reproducible fixture commands/reports suitable for CI and interview demonstration.
- [x] 7.4 Run targeted tests/race, complete Go tests/vet/build, deterministic report replay, Compose validation, and strict OpenSpec validation.
