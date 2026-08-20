## ADDED Requirements

### Requirement: Policy-controlled bounded over-budget recall
Recommendation policies MAY select Recall Provider budgets whose sum exceeds the absolute pre-rank
pool only when they include a valid `recommendation-provider-quota-merge` contract. In that case,
Frux SHALL apply readability-aware reservation-first deterministic mixing before unified feature
scoring, and every selected unique candidate SHALL reach the existing Ranker. Policies without that
contract remain subject to complete-pool budget-sum validation.

#### Scenario: Future policy selects wider recall
- **WHEN** a policy selects total Provider budgets above 500 and supplies a valid 500-candidate quota-merge contract
- **THEN** each Provider executes within its budget and the deterministic mixed pool of at most 500 candidates reaches unified scoring

#### Scenario: Wider policy lacks quota merge
- **WHEN** selected Provider budgets exceed 500 but quota fields are absent or invalid
- **THEN** the policy cannot become active

#### Scenario: Existing bootstrap policy is loaded
- **WHEN** `recommend/v1` or `recommend/v2` omits quota fields and its selected budgets total 500
- **THEN** Frux uses the prerequisite complete-pool behavior with no reservation or serialization change

#### Scenario: Healthy Provider fails to meet reservation
- **WHEN** a Provider succeeds but returns too few readable unique candidates
- **THEN** recommendation continues with its usable candidates, records bounded underfill, and deterministically reallocates remaining capacity during common fill
