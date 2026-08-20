## Why

Frux may eventually need a repeatable, privacy-bounded way to turn diagnostic delivery and outcome facts into an implementation-neutral training dataset. That need is not established by the current low-data roadmap, so this change is future-only and MUST remain inactive until an explicit activation gate proves a concrete training use, sufficient evidence, privacy approval, and an affordable resource envelope.

## What Changes

- Mark the exporter as a conditional future capability; no implementation task beyond activation review may start while the gate is unsatisfied, and current offline diagnosis/evaluation must not depend on it.
- Require a dated activation decision with a named training use, preregistered row/user/request counts and label/exposure coverage, privacy/security approval including deletion and opt-out handling, and database/runtime/storage budgets with owners.
- After activation only, add a read-only operator CLI command that exports a required bounded time window by joining durable diagnostic impressions to request-linked outcomes and rich behavior facts.
- Export only explicitly supported impression record and feature-schema versions, failing before publication when unsupported versions are present.
- Stream deterministic gzip JSONL rows and write an atomically published manifest containing dataset schema version, requested/effective time window, row and state counts, label definitions, source policy/model/schema versions, per-source watermarks, output checksum, split policy, and tool version.
- Require an operator-provided HMAC key and replace account identity with a stable export pseudonym while retaining bounded request/generation/video identity needed for grouping and leakage-safe splits.
- Use the shared `(user_key, request_key, generation, video_id)` identity contract and preserve zero-based generation-relative absolute position.
- Define delivered, exposed, and engaged states; deterministic label precedence; bounded watch ratio/effective watch time; and a rule that delivered-but-unexposed cards are never negative examples.
- Preserve both `occurred_at` and `recorded_at`: occurrence time controls behavior ordering and label windows, while recording time and per-source high-water marks control snapshot completeness and repeatability.
- Support deterministic time- and/or pseudonymous-user-based train/validation/test assignment, repeatable ordering, bounded resumable database pagination, cancellation, and cleanup of incomplete output.
- Document query/index expectations, export privacy and retention handling, CLI validation, integration coverage, and realistic fixtures.
- Keep the capability offline and read-only: it does not mutate recommendation facts, policy state, evidence, or retention.
- Explicitly exclude model training, current offline diagnosis/evaluation, semantic embeddings, pgvector, learned weights, exploration, and online serving. `evaluate-recommendation-policies-offline` must run on low-data replay/golden inputs without requiring this exporter.

## Capabilities

### New Capabilities

- `recommendation-training-dataset`: Conditionally activatable future export of supported diagnostic impressions and linked labels, with deterministic identity/time semantics, privacy checks, source watermarks, split assignment, streaming artifacts, atomic manifests, resumability, and operational safeguards.

### Modified Capabilities

None.

## Impact

- Inactive by default. Implementation depends on both an approved activation record and the implemented `persist-recommendation-training-impressions` identity/time/privacy contract.
- Adds an operator-only backend command plus read-only application/persistence interfaces for bounded export queries; no public HTTP or frontend contract changes.
- Reads PostgreSQL recommendation impressions, request-linked outcomes, and durable behavior facts using stable pagination and documented indexes/query plans.
- Produces local operator-managed dataset and manifest files only after activation; source retention remains unchanged and exported-file retention/deletion becomes an explicit approved operator responsibility.
- Adds focused CLI, label/split, persistence integration, cancellation/cleanup, checksum, determinism, fixture, and query-plan tests.
