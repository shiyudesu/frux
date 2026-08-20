## ADDED Requirements

### Requirement: Optional Evaluator Interoperability
If the future recommendation training dataset exporter is activated, its supported versions SHALL be able to provide the minimum trusted, privacy-bounded metadata needed for evaluator interoperability, but the evaluator SHALL NOT depend on that exporter.

#### Scenario: Candidate row is exported
- **WHEN** the exporter serializes a supported delivered recommendation impression
- **THEN** the row includes immutable generation, generation-relative position, canonical candidate `published_at`, stable domain-separated HMAC `author_key`, bounded topic/source identifiers, `degraded_state` as `healthy`, `degraded`, or `unknown`, and sorted bounded degraded providers when available

#### Scenario: Author grouping key is derived
- **WHEN** the source candidate has an author
- **THEN** `author_key` is lowercase hex HMAC-SHA-256 using the export key, the versioned `"frux:dataset:v1:author"` domain, and canonical author ID, while raw author identity and key material remain absent

#### Scenario: Degraded state is unavailable from trusted durable facts
- **WHEN** the exporter cannot resolve request degraded state from an available trusted durable record
- **THEN** it emits `unknown` with no degraded providers and never infers healthy state from absence

#### Scenario: Manifest describes replay metadata
- **WHEN** the dataset is published
- **THEN** its versioned manifest enumerates replay fields, occurred/recorded timestamp semantics, all source watermarks, author pseudonymization version, degraded-provider bounds, and supported source versions

#### Scenario: Dataset is exported repeatedly
- **WHEN** source facts, HMAC key, exporter version, and inputs are identical
- **THEN** publication time, author key, degraded fields, row bytes, gzip bytes, and manifest bytes are identical

#### Scenario: Dataset exporter remains inactive
- **WHEN** low-data offline evaluation is run before exporter activation
- **THEN** replay bundles and human golden sets provide the required metadata directly and evaluation remains fully supported
