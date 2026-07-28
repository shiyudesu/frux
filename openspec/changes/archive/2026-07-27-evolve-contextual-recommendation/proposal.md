## Why

GCFeed recommendation currently ranks one hot candidate pool with a 128-dimensional hash n-gram similarity, fixed weights, and limited context. It needs a richer but still operable recommendation pipeline that can use session behavior, multiple recall sources, negative signals, and versioned policy.

## What Changes

- Introduce multi-source candidate recall for freshness, popularity, content similarity, followed authors, and session continuation.
- Accept bounded recommendation context such as request/session identifiers, refresh index, recently viewed IDs, network class, and client capabilities.
- Expand user-interest construction to include reliable progress/completion, skip, interaction, follow, and explicit negative feedback with time decay.
- Add configurable and versioned ranking features, weights, eligibility filters, author/content diversity, and deterministic cursor semantics.
- Record recommendation request, policy version, candidate reason, score components, and downstream outcomes for offline evaluation.
- Preserve the current local hash-vector model as a fallback behind a model interface rather than requiring an external ML service immediately.

## Capabilities

### New Capabilities

- `contextual-recommendation`: Defines multi-recall candidate generation, contextual ranking, feedback signals, policy versioning, diversity, and evaluation data.

### Modified Capabilities

## Impact

- Affects recommendation Domain/Application/Persistence/HTTP code, Feed query context, exposure/view-event consumption, interaction/relation adapters, migrations, Redis caching, worker flows, and recommendation tests.
- Adds bounded request-context and recommendation-feedback contracts while retaining the existing recommendation Feed response shape.
- Requires updates to recommendation, Feed, exposure, interaction, architecture, optimization, and monitoring documentation.
