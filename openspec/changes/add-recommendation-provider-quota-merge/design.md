## Context

The prerequisite candidate-pool fix makes current bootstrap policies safe by requiring their
selected Provider budgets to total no more than the existing 500-candidate pre-rank ceiling and by
passing the complete unique merge to unified scoring. That restriction cannot support a future
sixth Provider or wider per-Provider recall without either increasing ranking/logging/Snapshot
bounds or reintroducing arbitrary runtime truncation.

Provider source scores are only meaningful inside one Provider, and concurrent completion order is
not a product rule. Frux therefore needs an explicit policy-controlled mixing algorithm before
future semantic or other Providers can safely expand the returned superset.

## Goals / Non-Goals

**Goals:**

- Add backward-compatible versioned pool-limit, Provider-order, and reservation policy fields.
- Allow selected budget sums above the global pool while keeping Ranker input at 50–500.
- Preserve Provider-local order, duplicate reasons, readable candidates, deterministic
  reservation, deterministic fill, and underfill recovery.
- Make equivalent facts produce identical mixed pools regardless of goroutine completion, map
  iteration, or raw slice order.
- Provide bounded metrics and diagnostics for quota behavior.

**Non-Goals:**

- Adding or naming a semantic/multimodal RecallProvider.
- Changing Provider query implementations, deadlines, capacity slots, final ranking, diversity,
  suppression, Snapshot, cursor, logging/evidence ceilings, or public APIs.
- Changing bootstrap v1/v2 serialization or rollout.
- Learning budgets/reservations from data.
- Persisting per-request candidate pools beyond existing bounded evidence/logging.

## Decisions

### 1. Add optional complete quota fields to PolicyConfiguration

The JSON contract adds `pre_rank_pool_limit`, `recall_provider_order`, and
`recall_provider_reservations` with omission-compatible serialization. If none are present, the
prerequisite complete-pool rule applies and selected budget sum must remain at most 500.

If any quota field is present, all are required:

- pool limit is 50–500;
- order contains every selected budget/deadline Provider exactly once and no unknown Provider;
- every selected Provider has a reservation entry from 0 through its budget;
- reservation sum is at most the pool limit;
- selected budget sum may exceed the pool limit.

All slices/maps are defensively normalized and cloned. Existing stored JSON and bootstrap policies
omit the fields and remain byte-for-byte unchanged.

Alternative: use map-key sorting as implicit order. Rejected because adding a Provider name would
silently change mixing priority and policy intent would not be explicit.

### 2. Normalize each Provider result before cross-Provider work

For each healthy Provider, the executor validates candidates, deduplicates video IDs, canonicalizes
that Provider's evidence, stably sorts by finite Provider source score, then publication time and
video ID, and applies its budget. Cross-Provider code never compares source scores.

Alternative: trust every Provider's returned slice. Rejected because deterministic merge must not
depend on implementation-specific order or accidental duplicates.

### 3. Revalidate the full bounded superset before mixing

The selected budget sum is bounded by selected Provider count times `MaxRecallBudget`; the registry
itself is bounded. Frux merges IDs/evidence and performs one visibility batch over the complete
unique superset before reservation/fill. Each Provider-local sequence is then filtered to readable
IDs while preserving order.

This makes reservation accounting truthful and avoids iterative query/refill cycles when a leading
candidate becomes unreadable.

Alternative: mix first, validate only selected candidates, then refill in rounds. Rejected because
it adds variable database round trips and can underfill despite remaining readable candidates.

### 4. Reservation representation is reason-based on globally unique candidates

The mixed pool stores each video once with all merged reasons. A selected candidate counts toward a
Provider's represented reservation when its merged reasons contain that Provider. Therefore an
overlapping candidate can represent multiple Providers while consuming one global slot.

Reservation phase iterates Providers in policy order across repeated rounds. A Provider with unmet
representation advances through its readable local sequence until it encounters a candidate that
either adds one global slot or is already selected and now counts as its representation. The phase
ends when all reservations are met, all relevant Provider sequences are exhausted, or the pool is
full. Valid reservation sums guarantee configuration capacity; runtime underfill comes only from
insufficient readable output.

Alternative: require Provider-exclusive candidates for reservations. Rejected because overlap is
valuable evidence, exclusive-only rules can waste high-quality shared candidates, and providers
with naturally similar recall would be penalized.

### 5. Fill remaining capacity with deterministic round-robin

After reservations, each Provider retains a cursor into its local sequence. Repeated fixed-order
rounds advance each non-exhausted Provider once. New IDs consume a slot; duplicates merge evidence
without consuming a slot; exhausted Providers leave the rotation. When only one remains, it may
fill the rest up to its budget and the pool limit.

The selected pool order is not the recommendation order. Unified feature ranking still establishes
the first product-visible global order.

### 6. Observe selection without persisting raw pools

Metrics use fixed Provider/result/phase/reason labels with bounded counts. Sampled request logs and
existing candidate evidence continue to store only their already specified bounded delivery/
diagnostic payloads; quota merge adds compact reason/phase summaries only where current schemas
allow them. It does not create a new per-request persistence table.

## Risks / Trade-offs

- [Visibility batch grows with selected budget sum] → Keep Provider count and per-Provider budget
  bounded, observe superset size/latency, and reject configurations beyond domain limits.
- [Overlapping candidates satisfy several reservations without adding topic diversity] → Treat
  reservation as Provider representation, not diversity; final author/content diversity remains a
  separate policy stage and metrics expose overlap.
- [Provider order creates intentional priority] → Make order explicit, versioned, validated, and
  visible in diagnostics instead of relying on accidental lexical/map order.
- [Quota fields alter bootstrap JSON] → Use omission-compatible fields and exact serialization
  regressions for v1/v2.
- [Round-robin implementation becomes stateful and error-prone] → Keep it as a pure function over
  normalized immutable inputs with exhaustive table/property-style tests.

## Migration Plan

1. Complete and deploy `fix-recommendation-candidate-pool-truncation`.
2. Add optional policy fields, normalization, cloning, persistence mapping, and bootstrap
   serialization regressions without selecting a quota policy.
3. Implement Provider-local normalization, visibility-filtered superset preparation, and the pure
   reservation/fill mixer behind policy opt-in.
4. Add metrics and focused tests, then create a disabled development policy whose budget sum
   exceeds its pool limit to validate the mixer.
5. Do not activate a new production policy or Provider in this change.

Rollback stops selecting policies with quota fields. Existing policies and complete-pool behavior
remain valid; no schema or data rollback is required because policy JSON is backward-compatible.

## Open Questions

None. Semantic Provider order, budget, reservation, and rollout values belong to the later Session
Semantic Recommendation proposal.
