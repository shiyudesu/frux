## MODIFIED Requirements

### Requirement: Typed hand-rolled routing
The frontend SHALL keep its hand-rolled history-based router, with routes expressed as a TypeScript union type so that invalid navigation targets fail type checking. The router SHALL include a typed `/search` pathname and validated search-query parsing without adding a routing library.

#### Scenario: Invalid route is a compile error
- **WHEN** a developer navigates to a misspelled or nonexistent route string
- **THEN** `tsc --noEmit` reports a type error

#### Scenario: Search route is authored
- **WHEN** frontend code navigates to the search destination
- **THEN** `/search` is accepted by the route union and its encoded query parameters are parsed through typed helpers

#### Scenario: Invalid search tab is supplied
- **WHEN** the URL contains an unsupported search tab value
- **THEN** the route safely normalizes it to the default video category
