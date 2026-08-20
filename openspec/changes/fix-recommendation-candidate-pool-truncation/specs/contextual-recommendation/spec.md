## ADDED Requirements

### Requirement: Complete bounded pre-rank candidate pool
For a normal policy-driven recommendation request, Frux SHALL pass every unique candidate returned
within the selected Provider budgets through visibility revalidation and unified feature scoring
before any global pool reduction. The pre-rank pool SHALL be bounded by the validated sum of
selected Provider budgets and an absolute maximum of 500 candidates, and MUST NOT be globally
ordered or truncated by `published_at`, video ID, Provider completion order, or response page size
before ranking.

#### Scenario: Small response page selects multiple Providers
- **WHEN** a 10-card request selects five Providers with budgets of 100 each and they return 500 distinct readable candidates
- **THEN** all 500 candidates are eligible for unified feature scoring before final ranking and page slicing

#### Scenario: Older candidate has stronger ranking features
- **WHEN** an older candidate is within its Provider budget and would outrank newer candidates after policy feature scoring
- **THEN** recency-only pre-rank processing cannot discard it before its features are evaluated

#### Scenario: Providers return the same video
- **WHEN** multiple Providers return one video within their budgets
- **THEN** the pre-rank pool contains one candidate retaining every valid Provider reason and source score, and the duplicate does not consume another global slot

#### Scenario: Selected budgets exceed the absolute pool bound
- **WHEN** a policy without an accepted quota-merge contract selects Provider budgets whose sum exceeds 500
- **THEN** policy validation rejects it before activation instead of silently truncating a Provider at request time

#### Scenario: Candidate becomes unreadable before ranking
- **WHEN** a recalled candidate is private, deleted, down, non-published, or media-unready at visibility revalidation
- **THEN** it is removed before feature loading and final ranking without allowing another recency-only reduction

#### Scenario: All healthy Providers return within bounds
- **WHEN** selected Providers complete successfully and their unique merged output is within the validated budget sum
- **THEN** Provider completion order and map iteration order do not change which candidates reach the Ranker

#### Scenario: Legacy no-policy caller requests a bounded pool
- **WHEN** a direct compatibility caller invokes recall without a versioned policy
- **THEN** Frux preserves its existing deterministic bounded result and exposure behavior without applying the policy-driven complete-pool contract
