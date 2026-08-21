# Human Recommendation Golden Set

- Rubric: `semantic-0-3-v1`
- Cases: 4
- Candidates: 10
- Agreement: 0.900000
- External model calls: 0

## Rankings

| Ranking | NDCG@1 | Direction accuracy | Suppression accuracy |
| --- | ---: | ---: | ---: |
| `baseline` | 0.285714 | 0.000000 | 0.000000 |
| `semantic` | 1.000000 | 1.000000 | 1.000000 |

## Limitations

- Golden Set labels are small human judgments and do not establish online causal lift.
- candidate presentation must remain blinded to policy name and rank during annotation.
- public dataset watch labels are not accepted as Frux Golden truth.
- the evaluator does not recommend or activate a policy.
