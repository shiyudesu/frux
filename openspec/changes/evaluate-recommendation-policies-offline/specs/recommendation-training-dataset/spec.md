## ADDED Requirements

### Requirement: Evaluator-Compatible Replay Metadata
Supported recommendation training datasets SHALL contain the minimum trusted, privacy-bounded metadata required to reproduce production tie-breaking, author diversity, and degraded slices without querying live services.

#### Scenario: Candidate row is exported
- **WHEN** the exporter serializes a supported delivered recommendation impression
- **THEN** the row includes canonical candidate `published_at`, a stable domain-separated HMAC `author_key`, bounded `degraded_state` as `healthy`, `degraded`, or `unknown`, and a sorted bounded list of degraded provider identifiers when available

#### Scenario: Author grouping key is derived
- **WHEN** the source candidate has an author
- **THEN** `author_key` is lowercase hex HMAC-SHA-256 using the export key, the versioned `"frux:dataset:v1:author"` domain, and canonical author ID, while raw author identity and key material remain absent

#### Scenario: Degraded state is unavailable from trusted durable facts
- **WHEN** the exporter cannot resolve request degraded state from an available trusted durable record
- **THEN** it emits `unknown` with no degraded providers and never infers healthy state from absence

#### Scenario: Manifest describes replay metadata
- **WHEN** the dataset is published
- **THEN** its versioned manifest enumerates the replay fields, timestamp semantics, author pseudonymization version, degraded-provider bounds, and supported source versions

#### Scenario: Dataset is exported repeatedly
- **WHEN** source facts, HMAC key, exporter version, and inputs are identical
- **THEN** publication time, author key, degraded fields, row bytes, gzip bytes, and manifest bytes are identical
