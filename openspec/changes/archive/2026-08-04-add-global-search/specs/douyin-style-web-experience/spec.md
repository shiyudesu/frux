## ADDED Requirements

### Requirement: Functional global search control
The shared application shell SHALL render the existing top search field as a functional form that submits typed navigation to public video and user search. The control SHALL support keyboard submission, an accessible name, responsive layouts, browser history synchronization, and truthful empty input behavior.

#### Scenario: Desktop user submits search
- **WHEN** a desktop user enters a valid term and activates the visible Search action
- **THEN** the application opens the typed search route with the encoded term

#### Scenario: Keyboard user submits search
- **WHEN** focus is in the top search input and the user presses Enter
- **THEN** the same search navigation occurs without triggering Feed keyboard shortcuts

#### Scenario: Mobile shell renders search
- **WHEN** the search route is viewed on a mobile viewport
- **THEN** the query, video/user tabs, results, and pagination controls remain reachable without horizontal overflow
