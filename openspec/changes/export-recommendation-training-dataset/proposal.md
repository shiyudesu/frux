## Why

Frux needs a repeatable, privacy-bounded way to turn durable recommendation delivery and outcome facts into an implementation-neutral offline dataset without granting analytics tooling write access to production facts. This change depends on the active planned change `persist-recommendation-training-impressions`, whose versioned delivered-card records provide the authoritative export base.

## What Changes

- Add a read-only operator CLI command that exports a required bounded time window by joining durable recommendation training impressions to request-linked outcomes and rich behavior facts.
- Export only explicitly supported impression record and feature-schema versions, failing before publication when unsupported versions are present.
- Stream deterministic gzip JSONL rows and write a manifest containing dataset schema version, requested/effective time window, row and state counts, label definitions, source policy/model/schema versions, output checksum, split policy, and tool version.
- Require an operator-provided HMAC key and replace account identity with a stable export pseudonym while retaining bounded request/video identity needed for grouping and leakage-safe splits.
- Define delivered, exposed, and engaged states; deterministic label precedence; bounded watch ratio/effective watch time; and a rule that delivered-but-unexposed cards are never negative examples.
- Support deterministic time- and/or pseudonymous-user-based train/validation/test assignment, repeatable ordering, bounded resumable database pagination, cancellation, and cleanup of incomplete output.
- Document query/index expectations, export privacy and retention handling, CLI validation, integration coverage, and realistic fixtures.
- Keep the capability offline and read-only: it does not mutate recommendation facts, policy state, evidence, or retention.
- Explicitly exclude model training, offline policy scoring/evaluation, semantic embeddings, pgvector, learned weights, exploration, and online serving. The later `evaluate-recommendation-policies-offline` change will consume this export rather than querying production facts directly.

## Capabilities

### New Capabilities

- `recommendation-training-dataset`: Deterministic, privacy-bounded offline export of supported recommendation impressions and linked labels, including version checks, split assignment, streaming artifacts, manifests, resumability, and operational safeguards.

### Modified Capabilities

None.

## Impact

- Depends on the not-yet-implemented `persist-recommendation-training-impressions` change and its `recommendation-training-impressions` contract; implementation must sequence after that source schema and required indexes exist.
- Adds an operator-only backend command plus read-only application/persistence interfaces for bounded export queries; no public HTTP or frontend contract changes.
- Reads PostgreSQL recommendation impressions, request-linked outcomes, and durable behavior facts using stable pagination and documented indexes/query plans.
- Produces local operator-managed dataset and manifest files containing only the enumerated privacy-bounded schema; source retention remains unchanged and exported-file retention becomes an explicit operator responsibility.
- Adds focused CLI, label/split, persistence integration, cancellation/cleanup, checksum, determinism, fixture, and query-plan tests.
