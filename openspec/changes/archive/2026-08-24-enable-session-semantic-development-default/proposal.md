## Why

Frux has already proved Session Semantic Recommendation under a one-percent active cohort, but a
low-traffic development project will almost never exercise that path during normal use. Development
should run the accepted semantic policy for every recommendation request while production remains
explicitly opt-in and fail-closed.

## What Changes

- Enable the Session-only multimodal runtime by default in development Docker Compose using the
  registered default profile, while preserving environment overrides and leaving production off.
- Extend the registered Session Semantic rollout policy to support an explicitly acknowledged 100%
  development rollout without weakening ordinary staged-rollout limits.
- Create a new immutable v4 policy for full development rollout instead of modifying the accepted v3
  policy in place, and exact-disable other semantic rollout targets after v4 activation.
- Add an idempotent development startup reconciler that ensures the compatible v4 policy exists and is
  active whenever development full rollout is configured.
- Update existing-policy acceptance so a 100% target requires a retained enabled emergency baseline
  but does not require an impossible non-target cohort request.
- Document that development requests use Session Semantic by default, while production still requires
  explicit runtime and policy activation.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `multimodal-provider-runtime`: Development Compose starts the Session-only runtime by default while
  production and explicit false overrides remain off.
- `session-semantic-rollout`: Adds an explicitly development-scoped 100% rollout path, immutable v4
  activation, and exact retirement of other semantic rollout targets.
- `session-semantic-acceptance-runner`: Allows full-cohort target verification while retaining an
  enabled baseline for exact-disable recovery.

## Impact

- Affects development Compose defaults, multimodal configuration, rollout policy construction and
  validation, API startup composition, existing-policy acceptance, tests, and recommendation docs.
- Adds no model training, Adapter dependency, external model call during recommendation, database
  schema, public API, ANN index, or production-default enablement.
- Existing v1/v2 baselines and the historical v3 row remain retained; the development runtime selects
  the higher compatible v4 policy for all requests.
