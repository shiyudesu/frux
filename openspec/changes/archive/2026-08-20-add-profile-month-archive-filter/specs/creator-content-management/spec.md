## ADDED Requirements

### Requirement: Creator Work Archive Months
Frux SHALL provide an authenticated creator archive-month read that returns the unique UTC creation months containing the owner's non-deleted works for a required public or private visibility. Months SHALL use canonical `YYYY-MM` values, be ordered newest first, and remain independent from keyword and cursor state.

#### Scenario: Creator loads public archive months
- **WHEN** an authenticated creator requests archive months with public visibility
- **THEN** Frux returns each UTC creation month containing at least one of that creator's non-deleted public-visible works exactly once in newest-first order

#### Scenario: Creator loads private archive months
- **WHEN** an authenticated creator requests archive months with private visibility
- **THEN** Frux returns months derived only from that creator's non-deleted private works and does not expose another user's archive

#### Scenario: Visibility is invalid
- **WHEN** an archive-month request supplies a visibility other than public or private
- **THEN** Frux rejects the request as invalid without querying or returning creator work metadata

#### Scenario: Creator has no matching works
- **WHEN** the authenticated creator has no non-deleted works for the requested visibility
- **THEN** Frux returns a successful response with an empty month list

#### Scenario: Archive persistence read fails
- **WHEN** PostgreSQL cannot complete the archive-month query
- **THEN** Frux returns an explicit service error and does not return a success-shaped empty list

### Requirement: Profile Archive Month Query Compatibility
The Web SHALL translate a selected canonical archive month into the inclusive UTC first and last date of that month and SHALL submit those values through the existing creator video's `created_from` and `created_to` filters. The existing range-query API SHALL remain available to compatible clients.

#### Scenario: Creator selects an archive month
- **WHEN** the Web applies archive month `2026-08`
- **THEN** it resets the active creator cursor and queries with `created_from=2026-08-01` and `created_to=2026-08-31`

#### Scenario: Creator clears the archive month
- **WHEN** the creator selects `全部`
- **THEN** the Web queries the active visibility with empty creation-date bounds and preserves the visible keyword filter

#### Scenario: Existing client submits an arbitrary range
- **WHEN** a compatible client submits valid `created_from` and `created_to` values directly
- **THEN** the existing inclusive creator range query continues to validate, filter, order, and paginate as before
