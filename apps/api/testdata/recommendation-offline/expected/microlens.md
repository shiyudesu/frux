# Offline Recommendation Evaluation

- Dataset: `microlens`
- Release: `synthetic-canonical-schema-fixture`
- Schema: `microlens-canonical-v1`
- Session profile: `short-video-session-v1`
- Cases: 2
- External model calls: 0

## Baselines

| Baseline | Cases | HitRate@1 | NDCG@1 | MRR | Catalog coverage |
| --- | ---: | ---: | ---: | ---: | ---: |
| `popularity` | 2/2 | 0.500000 | 0.500000 | 0.666667 | 0.666667 |
| `recent_interaction` | 2/2 | 0.000000 | 0.000000 | 0.416667 | 0.666667 |
| `category` | 2/2 | 1.000000 | 1.000000 | 1.000000 | 0.666667 |
| `text` | 2/2 | 1.000000 | 1.000000 | 1.000000 | 0.666667 |
| `image` | 2/2 | 1.000000 | 1.000000 | 1.000000 | 0.666667 |
| `multimodal` | 2/2 | 1.000000 | 1.000000 | 1.000000 | 0.666667 |
| `multimodal_session` | 2/2 | 1.000000 | 1.000000 | 1.000000 | 0.666667 |

## Performance evidence

- Unavailable: checksum-covered performance evidence not declared

## Limitations

- offline results are non-causal and do not establish production lift.
- dataset user and item namespaces remain isolated and are not combined into one score.
- public watch labels do not replace blinded Frux Golden Set judgments.
- the evaluator does not recommend, activate, shadow, or roll out a policy.
