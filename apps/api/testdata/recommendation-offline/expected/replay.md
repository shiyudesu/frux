# Production Recommendation Replay

- Scope: `full_pool_fixture`
- Cases: 1
- Baseline parity: true
- Comparative metrics available: true
- External model calls: 0

## Candidate replay

| Policy | Mean absolute rank shift |
| --- | ---: |
| `candidate` | 0.666667 |

## Limitations

- replay proves scorer compatibility over frozen candidates and is not a recall or causal-lift estimate.
- served-subset replay cannot infer absent candidates or counterfactual outcomes.
- non-replayable policy differences suppress comparative metrics.
- the evaluator does not recommend or activate a policy.
