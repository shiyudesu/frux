# recommendation-provider-quota-merge Specification

## Purpose

Define the versioned, deterministic, readability-aware Provider quota contract that allows bounded
recall budgets to exceed the global pre-rank pool without introducing arbitrary truncation.

## Requirements

### Requirement: Versioned Provider quota policy
Frux SHALL support an optional complete quota-merge policy consisting of a bounded pre-rank pool
limit, an ordered list containing every selected Recall Provider exactly once, and a non-negative
reservation for every selected Provider that does not exceed its recall budget. The pool limit
SHALL be between 50 and 500, and the sum of reservations MUST NOT exceed the pool limit.

#### Scenario: Complete quota policy is valid
- **WHEN** provider order exactly matches selected budget/deadline Providers, every reservation is within its budget, and reservation sum is within the pool limit
- **THEN** the policy may activate deterministic quota merge even when selected budgets total more than the pool limit

#### Scenario: Quota fields are partial
- **WHEN** a policy supplies only a pool limit, order, or reservation map, or omits a selected Provider from any required field
- **THEN** policy validation rejects the configuration

#### Scenario: Reservation exceeds bounds
- **WHEN** a reservation is negative, exceeds its Provider budget, references an unknown Provider, or total reservations exceed the pool limit
- **THEN** policy validation rejects the configuration

#### Scenario: Existing policy omits quota fields
- **WHEN** a stored or bootstrap policy contains none of the quota-merge fields and its selected budgets fit the complete-pool ceiling
- **THEN** Frux preserves complete bounded pre-rank scoring without changing policy serialization or activating quota mixing

### Requirement: Deterministic Provider-local normalization
Before cross-Provider mixing, Frux SHALL validate, deduplicate, stably order, and bound each healthy
Provider's result by that Provider's policy budget. Provider-local order SHALL use only that
Provider's finite source score followed by deterministic publication/video tie-breaking and MUST
NOT compare source-score scales across Providers.

#### Scenario: Provider returns more than its budget
- **WHEN** a Provider returns more valid candidates than its configured budget
- **THEN** only its stable Provider-local prefix participates in quota merge

#### Scenario: Provider result order changes
- **WHEN** equivalent Provider candidates arrive in a different raw slice order
- **THEN** Provider-local normalization produces the same candidate sequence

#### Scenario: Provider emits duplicate IDs
- **WHEN** one Provider returns the same video more than once
- **THEN** its local sequence contains one candidate with valid canonical Provider evidence

### Requirement: Reservation-first deterministic mixing
Frux SHALL merge duplicate IDs across Providers while retaining all valid reasons/source scores,
then satisfy Provider reservations through deterministic rounds in policy Provider order. A global
video SHALL consume at most one pool slot; when it carries multiple selected Provider reasons it may
count toward each represented Provider's reservation. A Provider that lacks enough readable unique
candidates SHALL underfill without taking reserved slots from another Provider after it is
exhausted.

#### Scenario: Every Provider can satisfy reservation
- **WHEN** each selected Provider has enough readable candidates and the pool has capacity
- **THEN** the mixed pool contains at least its configured represented-candidate reservation before common fill

#### Scenario: Providers overlap
- **WHEN** two Providers return the same video and it is selected
- **THEN** it occupies one global slot, retains both reasons, and may count as selected representation for both Providers

#### Scenario: Provider underfills
- **WHEN** a Provider has fewer readable candidates than its reservation
- **THEN** all of its usable candidates survive, the underfill is observed, and unused capacity becomes available to common fill

#### Scenario: Provider completion order changes
- **WHEN** concurrent Providers complete in another order with equivalent normalized outputs
- **THEN** the reservation result is identical

### Requirement: Deterministic round-robin fill
After the reservation phase, Frux SHALL fill remaining pre-rank pool slots by deterministic
round-robin over policy Provider order and remaining Provider-local sequences until the pool limit
is reached or all Providers are exhausted. Duplicate encounters SHALL merge evidence without
consuming another slot, and no Provider SHALL gain priority from request completion speed or Go map
iteration order.

#### Scenario: Reservations leave unused capacity
- **WHEN** the reservation phase selects fewer candidates than the pool limit
- **THEN** remaining unique candidates are added through fixed Provider-order rounds

#### Scenario: One Provider has many more candidates
- **WHEN** one Provider has substantial remaining output while other Providers remain non-empty
- **THEN** it cannot consume all remaining slots before each other Provider receives its deterministic round opportunity

#### Scenario: All other Providers are exhausted
- **WHEN** only one Provider retains unseen candidates
- **THEN** it fills the remaining pool up to its budget and the global limit

### Requirement: Readability-aware quota merge
Only candidates revalidated as currently published, public, and media-ready SHALL participate in
reservation and fill accounting. Readability filtering SHALL operate on the complete bounded
Provider-output superset before final mixing, so an unreadable reserved candidate cannot waste a
slot while another readable candidate from that Provider remains.

#### Scenario: Leading Provider candidate becomes private
- **WHEN** an early Provider-local candidate fails readability revalidation but a later candidate remains readable
- **THEN** quota merge advances to the later readable candidate when satisfying reservation or fill

#### Scenario: Visibility lookup fails
- **WHEN** Frux cannot revalidate the bounded candidate superset
- **THEN** recommendation fails through the existing safe load-error path rather than mixing unverified candidates

### Requirement: Quota merge observability
Frux SHALL expose fixed-cardinality metrics and sampled bounded diagnostics for Provider returned,
local unique, readable, reserved, fill-selected, final represented, overlap, and underfill counts.
Metrics and normal logs MUST NOT include candidate IDs, user/request/session IDs, candidate lists,
source-score values as labels, or raw Provider errors.

#### Scenario: Provider reservation underfills
- **WHEN** a selected Provider cannot reach its reservation
- **THEN** fixed-label metrics identify the Provider and closed underfill reason with bounded counts

#### Scenario: Candidate has multiple reasons
- **WHEN** one selected candidate represents several Providers
- **THEN** overlap and representation metrics reflect the merge without emitting the video ID
