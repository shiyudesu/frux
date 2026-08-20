## Context

Frux currently has `hash-ngram-v1` as a cheap, deterministic content-vector fallback. The original
semantic plan added a locally hosted Python/MiniLM runtime. The confirmed direction is to use a
managed external Embedding API instead, so Frux must own the data boundary, adapter contract,
validation, resilience, and model identity without owning model artifacts or inference processes.

This is recommendation-roadmap step 5. It establishes reusable primitives only. Actual provider
calls occur later from durable live semantic jobs and the operator backfill; synchronous publish,
API, Feed, profile, and ranking paths must never call the provider.

## Goals / Non-Goals

**Goals:**

- Define a narrow Go interface independent of any provider SDK or wire format.
- Pin one provider/model/revision/dimension/canonicalizer tuple per deployment.
- Define `semantic-text-v1` and stable text hashing independently from provider tokenization.
- Minimize outbound data to normalized public title/description text only.
- Validate every returned vector before it can enter a cache or repository.
- Bound timeout, concurrency, rate-limit handling, circuit behavior, quota, and cost.
- Make duplicate canonical text reusable through contract-scoped hash caching.
- Make any contract change require a separately identified rebuild.

**Non-Goals:**

- Hosting, downloading, converting, training, or fine-tuning a model.
- Python, PyTorch, Sentence Transformers, model containers, CPU inference workers, or GPUs.
- Performing inference in an HTTP handler, Feed request, publication transaction, or Kafka handler.
- Owning durable live jobs or historical backfill orchestration.
- Adding vector search, semantic profiles, retrieval, ranking, or policy changes.
- Replacing or deprecating `hash-ngram-v1`.

## Decisions

### 1. Use a narrow Go port with provider adapters

The application-facing port accepts only a batch of already canonicalized public texts and returns
vectors in the same positional order:

```go
type SemanticEmbedder interface {
    Embed(ctx context.Context, inputs []CanonicalText) (EmbeddingBatch, error)
}
```

`CanonicalText` contains canonical text and its `semantic-text-v1` hash. It contains no user ID,
video ID, request ID, URL, token, behavior field, lifecycle field, or provider option. The adapter
maps this neutral shape to one configured provider API. Provider SDK types remain in
infrastructure; application and domain packages depend only on the narrow port and bounded error
classes.

The adapter performs one network attempt. It never hides retries. Durable live jobs and backfills
own retry count, delay, lease/checkpoint safety, and terminal classification.

### 2. Pin one immutable deployment contract

Startup requires bounded configuration for:

- provider identifier;
- API base endpoint selected by the adapter;
- model identifier;
- immutable model revision or provider snapshot identifier;
- exact output dimension;
- canonicalizer `semantic-text-v1`;
- request timeout, maximum batch, maximum in-flight requests, and rate limit;
- provider pricing revision and bounded budget/quota settings.

Requests cannot override any of these fields. The complete tuple
`(provider, model, revision, dimension, canonicalizer)` is the semantic identity. The full tuple is
returned with validated results and must be persisted by downstream jobs and vector rows.
Credentials are injected separately from the secret/config mechanism and are never part of that
identity.

A provider, model, revision, dimension, or canonicalizer change creates a new identity. It cannot
rewrite existing rows in place or silently become compatible. Deployment of the new identity
requires a planned rebuild/backfill, coverage validation, and explicit consumer cutover while
`hash-ngram-v1` remains available.

### 3. Define `semantic-text-v1` independently from providers

The canonicalizer:

1. accepts title and description only after the caller has established that the video is published
   and public;
2. normalizes each field with Unicode NFKC;
3. trims edges and collapses each Unicode-whitespace run to one ASCII space;
4. rejects control, surrogate, and invalid Unicode scalar values;
5. enforces title length 1–200 code points and description length 0–2,000 code points;
6. composes `title` when description is empty, otherwise `title + "\n" + description`;
7. computes lowercase SHA-256 over UTF-8 bytes of
   `"semantic-text-v1\n" + canonical_text`.

Provider tokenization, truncation, prefixes, and billing transformations are adapter concerns and
cannot change the canonical text or hash. If provider-specific prefixes are required, they are a
fixed part of that adapter/model revision and are covered by contract tests.

### 4. Enforce a minimal outbound privacy boundary

The provider request contains only canonical text strings in batch order and fixed model selection
required by the provider. It never contains Frux user IDs, video/business IDs, request or trace IDs,
interaction/behavior data, access tokens, JWTs, internal tokens, source URLs, object-store URLs,
private/unpublished drafts, or arbitrary metadata.

The adapter correlates results by local position; it does not send business identifiers as provider
item IDs. Logs and metrics contain no raw/canonical text or text hash. Request/response capture,
provider payload logging, and SDK debug logging are disabled. Provider credentials come only from
secret/config injection and are not stored in PostgreSQL, Redis, Kafka, cache entries,
checkpoints, logs, traces, or metrics.

### 5. Deduplicate and cache by canonical text hash

Before an API call, the service deduplicates the batch by
`(provider, model, revision, dimension, canonicalizer, text_hash)`. A narrow vector-cache port may
return a previously validated vector for that exact key. Cache entries contain only the complete
contract identity, text hash, validated vector, and bounded timestamps; they do not contain raw
text or credentials.

Cache hits are validated with the same dimension, finiteness, and norm rules as provider results.
Corrupt or mismatched entries are ignored and reported with a bounded result. Successful provider
results are inserted idempotently. Cache failure may reduce reuse but must not weaken vector
validation or cause an online request-path call.

### 6. Validate the complete provider response

The adapter bounds request and response bodies and requires exactly one result for each unique input
in deterministic order. It rejects partial, missing, duplicated, reordered, or extra items and any
response that cannot be tied to the configured model contract.

Each vector must have the exact configured dimension, contain only finite values, and have a
positive norm. The service applies deterministic L2 normalization, then verifies unit norm within
`1e-5`. A batch is atomic: if one vector fails, none from that provider response enters cache or
persistence. Defensive normalization does not make a wrong dimension, NaN, infinity, zero vector,
or wrong model acceptable.

### 7. Bound timeout, rate limits, retry hints, and the circuit gate

Configuration sets one end-to-end provider timeout, maximum batch size, maximum in-flight requests,
and local QPS/burst limits within documented safe ranges. Capacity is acquired before a payload is
built. Cancellation stops the request and releases capacity.

Provider `429`/quota responses preserve only a parsed, bounded `Retry-After` duration; response
bodies and headers are not exposed. Network failure, timeout, `429`, and bounded `5xx` results are
retryable by durable callers. Invalid input, authentication/authorization, unknown model/revision,
dimension/contract mismatch, malformed response, and local configuration are terminal until an
operator changes configuration or manually requeues work.

A replica-local circuit/gate opens after a bounded rolling threshold of retryable failures or
immediately on authentication, quota exhaustion, or contract mismatch. While open, calls fail fast
with a bounded class. Half-open probes are rate-limited. The gate never blocks API/Feed startup or
hash work because only asynchronous workers/backfills invoke it.

### 8. Measure quota, cost, and safety without high-cardinality data

Metrics cover calls, unique texts, cache hits/misses, duration, result class, throttling,
`Retry-After`, circuit state, input code points, provider billable units, estimated cost, quota
remaining, and budget-gate pauses. Labels use closed registries and do not include provider/model
strings, IDs, text, hashes, URLs, credentials, raw errors, or retry numbers.

Pricing uses an explicitly configured provider pricing revision. A local estimator computes
billable units/cost before calls for budgeting; actual provider-reported usage, when available, is
recorded after validation. Missing or stale pricing prevents cost-authorized backfill execution but
does not affect `hash-ngram-v1`.

### 9. Keep all invocation asynchronous

This change supplies the canonicalizer, identity, port, adapter, validation, cache contract, and
operational controls. It exposes no public or internal embedding HTTP endpoint. The later live
integration may invoke the port only after claiming a durable PostgreSQL semantic job. The backfill
may invoke it only inside a resumable operator run.

Publication, Feed, hash generation, and API handlers may create or observe durable state but cannot
wait for the provider. Provider failure therefore changes semantic freshness only; it cannot fail a
publish request, block Feed, or remove hash fallback.

## Risks / Trade-offs

- [Provider outage or throttling creates backlog] -> Durable callers own retry timing; local gates
  fail fast and `Retry-After` is bounded.
- [Provider silently changes a model] -> Pin an immutable revision/snapshot, validate dimension and
  fixtures, and require a new identity plus rebuild for any change.
- [Sensitive context leaks externally] -> Send only canonical published/public title/description
  text and prohibit all identifiers, behavior, URLs, drafts, and credentials.
- [Duplicate text wastes quota] -> Deduplicate and cache by the full contract plus text hash.
- [Pricing or quota changes unexpectedly] -> Version pricing configuration, expose cost/quota
  metrics, and stop cost-authorized work when the budget gate is closed.
- [Managed API dependence reduces local control] -> Keep the adapter replaceable and
  `hash-ngram-v1` permanently available.

## Migration Plan

1. Verify roadmap prerequisites are complete and archived.
2. Add the neutral canonicalizer, contract identity, error taxonomy, embedder/cache ports, and unit
   tests.
3. Add one configured provider adapter with secret-only credentials, bounded transport, strict
   validation, cost estimator, and circuit/gate tests.
4. Add configuration, metrics, redaction, provider sandbox/fixture contract tests, and
   documentation.
5. Integrate only through durable live jobs and the resumable backfill changes.
6. For any future contract change, deploy a new identity, rebuild coverage, validate it, then
   explicitly cut consumers over; never mutate existing identity rows in place.

Rollback disables provider calls or removes the adapter configuration. Existing semantic facts and
all `hash-ngram-v1` rows remain intact.

## Open Questions

None. Provider selection is adapter configuration, while identity pinning, privacy, canonicalization,
validation, resilience, asynchronous-only use, and rebuild requirements are fixed.
