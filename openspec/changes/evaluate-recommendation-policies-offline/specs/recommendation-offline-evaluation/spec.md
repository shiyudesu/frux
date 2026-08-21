## ADDED Requirements

### Requirement: Evaluation tracks are offline and independent
The evaluator SHALL expose independent public-dataset, production-replay, and human-Golden tracks
that run without Frux runtime services, business-database access, policy mutation, or model calls.

#### Scenario: One track is selected
- **WHEN** an operator runs a valid track with bounded local inputs
- **THEN** only that track's inputs and metrics are required and its report declares `external_model_calls: 0`

#### Scenario: Runtime dependency is requested
- **WHEN** an input or option attempts to use PostgreSQL, Redis, Kafka, HTTP, S3, or a model provider
- **THEN** evaluation fails before processing rather than contacting the dependency

### Requirement: Public datasets remain isolated and provenance-bound
The evaluator SHALL require a strict manifest containing dataset kind, release, source, citation,
license acknowledgement, schema versions, relative file paths, SHA-256 hashes, and row counts. It
MUST NOT combine user or item namespaces across datasets or redistribute/download upstream data.

#### Scenario: Manifest is valid
- **WHEN** every declared file is regular, contained by the dataset root, hash/count matched, and supported
- **THEN** the adapter emits dataset-local opaque user/item keys and records bounded provenance in the report

#### Scenario: Manifest or path is unsafe
- **WHEN** a file escapes the root, uses an unsupported release/schema, is undeclared, or fails hash/count validation
- **THEN** evaluation stops with a closed input failure and emits no partial metric report

### Requirement: KuaiRec v2 inputs are parsed strictly
The KuaiRec adapter SHALL parse documented interaction identity, durations, timestamp, watch ratio,
and item-category fields, validate bounded values, and preserve absence of unsupported labels.

#### Scenario: Interaction is compatible
- **WHEN** a KuaiRec row has valid user/video identity, positive durations, canonical timestamp, and finite watch ratio consistent with durations
- **THEN** it is normalized into one dataset-local interaction record

#### Scenario: Like label is absent
- **WHEN** KuaiRec provides no native like signal
- **THEN** the evaluator does not synthesize or report a like metric from field absence

### Requirement: MicroLens releases use an explicit canonical manifest
The MicroLens adapter SHALL consume a canonical release export whose manifest declares the official
source release, normalization recipe/version, interaction/item schemas, and optional precomputed
feature channels. It MUST NOT guess an unknown raw MicroLens layout.

#### Scenario: Canonical export is compatible
- **WHEN** interaction and item files match `microlens-canonical-v1` and their declared release provenance
- **THEN** the evaluator normalizes them without requiring raw media or model inference

#### Scenario: Raw layout is undeclared
- **WHEN** files do not provide a supported canonical recipe/schema
- **THEN** the adapter rejects them and identifies the unsupported profile without inspecting arbitrary columns heuristically

### Requirement: Chronological session cases avoid future leakage
The evaluator SHALL build cases through a versioned chronological profile that uses only interactions
earlier than a held-out positive target, bounds session history, and distinguishes positive,
quick-skip, neutral, and missing feedback.

#### Scenario: User is eligible
- **WHEN** a user has a held-out positive target, sufficient earlier history, and the target exists in the eligible candidate universe
- **THEN** one deterministic case is created using only earlier interactions and excluding previously interacted candidates

#### Scenario: Feedback is neutral or missing
- **WHEN** watch ratio lies between preregistered positive/quick-skip thresholds or is unavailable
- **THEN** it is not converted into a positive target or negative session signal and its treatment is counted explicitly

### Requirement: Baselines are deterministic and availability-aware
The evaluator SHALL register Popularity, Recent Interaction, Category, Text-only, Image-only,
Multimodal, and Multimodal + Session Interest baselines with fixed scoring/tie-break contracts and
explicit feature requirements.

#### Scenario: Required inputs exist
- **WHEN** a case and candidate universe contain all fields required by a baseline
- **THEN** the baseline returns a deterministic item order with dataset-local item key ascending as the final tie-break

#### Scenario: Required inputs are incomplete
- **WHEN** vectors, categories, timestamps, or other required fields are missing
- **THEN** the baseline/case is marked unavailable with coverage and exclusion counts rather than zero-filling or silently dropping fields

### Requirement: Public-dataset ranking metrics have explicit denominators
The evaluator SHALL compute Recall@K, NDCG@K, HitRate@K, MRR, Catalog Coverage, available
category/author diversity and concentration, repetition, feature coverage, and deterministic ranking
work for sorted unique K values. Exact latency MAY appear only as checksum-covered optional evidence.

#### Scenario: Baseline cases complete
- **WHEN** at least one eligible case is ranked
- **THEN** every metric includes its numerator/denominator or sample count and catalog metrics use the declared eligible universe

#### Scenario: Metadata metric is unsupported
- **WHEN** author, category, feature, or throughput metadata is absent
- **THEN** the metric is `unavailable` with a reason and is not serialized as numeric zero

#### Scenario: Runtime latency is not declared
- **WHEN** no checksum-covered performance evidence file is present
- **THEN** the canonical report remains byte-stable, includes deterministic ranking work, and marks Exact latency unavailable

### Requirement: Production replay requires exact compatible parity
The replay track SHALL validate production policy configurations and replay frozen score components,
tie-breaking, and diversity. Comparative metrics MUST be suppressed when differences are not
replayable or canonical baseline parity is not exact.

#### Scenario: Canonical replay is compatible
- **WHEN** baseline order matches exactly and candidates differ only in replayable weight/diversity fields
- **THEN** the evaluator emits deterministic baseline/candidate replay metrics and difference inventory

#### Scenario: Difference is non-replayable
- **WHEN** recall, deadlines, feature generation, suppression, fallback, rollout, sampling, retention, contract, or Snapshot behavior differs
- **THEN** normal replay fails; diagnostic-only mode may list differences but cannot rank or recommend a policy

### Requirement: Human Golden Sets are blinded and versioned
The Golden track SHALL validate versioned Query, Similar Video, Session Direction, and Negative
Suppression cases with blinded 0-3 judgments, at least two annotators, and required adjudication for
large disagreements.

#### Scenario: Golden annotations are complete
- **WHEN** provenance, rubric, independent judgments, and required adjudication are valid
- **THEN** the evaluator reports agreement, label coverage, semantic NDCG, direction accuracy, and suppression accuracy

#### Scenario: Public label is supplied as Golden truth
- **WHEN** an input attempts to reuse a public-dataset watch label as a Frux semantic judgment
- **THEN** validation rejects the cross-track label provenance

### Requirement: Reports are deterministic, atomic, and non-causal
The evaluator SHALL atomically produce permission-restricted canonical JSON and Markdown reports
containing provenance, versions, hashes/counts, split/exclusion summaries, baseline availability,
metric definitions/denominators, latency, warnings, and limitations without raw rows, histories,
vector components, credentials, absolute operator paths, or wall-clock-dependent fields.

#### Scenario: Identical evaluation is repeated
- **WHEN** inputs, options, tool version, and output paths are identical
- **THEN** JSON and Markdown bytes are identical and existing outputs are replaced only after both new reports are durable

#### Scenario: Report is interpreted
- **WHEN** any track completes
- **THEN** the report states that results are offline/non-causal, datasets remain separate, no statistical production lift is claimed, and no policy promotion is recommended automatically

### Requirement: Training and online activation remain separate gates
The evaluator SHALL remain usable without training exports or learned weights and SHALL NOT train,
optimize, activate, Shadow, or Rollout recommendation policies.

#### Scenario: Semantic roadmap continues
- **WHEN** offline evidence is produced before training gates pass
- **THEN** no exporter, optimizer, long-term profile, HNSW index, or online experiment is required

#### Scenario: Later training work is approved
- **WHEN** a separate future change consumes an evaluation report
- **THEN** that change owns dataset export, learning, statistical power, privacy review, and promotion decisions
