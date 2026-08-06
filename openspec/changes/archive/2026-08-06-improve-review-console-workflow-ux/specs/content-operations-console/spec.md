## MODIFIED Requirements

### Requirement: Review Operations Workspace
The admin shell SHALL provide task-oriented review views for available work, the current reviewer's in-progress work, and recently completed work, with protected video preview, truthful machine-evidence provenance, automatic bounded keep-alive, explicit release, approve, and reject actions. User-visible copy SHALL describe review tasks and occupancy without exposing case, claim, immutable-history, lease-token, or renewal terminology as primary workflow labels.

#### Scenario: Reviewer opens available work
- **WHEN** a reviewer opens the review workspace
- **THEN** the page labels rows as review tasks, shows only currently available work in the default scope, and provides a “开始审核” action

#### Scenario: Reviewer starts a task
- **WHEN** the reviewer successfully starts an available task
- **THEN** the task appears in “我正在审核”, its protected video preview is available, and the page displays the server-derived occupancy expiry

#### Scenario: Reviewer reloads an owned task
- **WHEN** the current reviewer reloads an in-progress task while its assignment remains valid
- **THEN** the page resumes the assignment through a rotated one-time token and re-enables authorized actions

#### Scenario: Reviewer keeps a task open
- **WHEN** an owned task remains actively open and the occupancy interval approaches its renewal point
- **THEN** the page automatically requests a bounded extension and updates the visible expiry without exposing the opaque token

#### Scenario: Reviewer releases a task
- **WHEN** the current reviewer selects “放回待处理” and release succeeds
- **THEN** the task leaves “我正在审核”, becomes available to eligible reviewers, and the page confirms the released state

#### Scenario: Reviewer decides an owned task
- **WHEN** the reviewer has a valid assignment and submits a valid decision
- **THEN** the page displays the committed final state, stops keep-alive, and moves the task to recently completed work

#### Scenario: Occupancy expires during review
- **WHEN** a decision or keep-alive returns the stable assignment-expired conflict
- **THEN** the page preserves the inspected evidence, disables stale actions, and offers a fresh available-work reload or resume when still eligible

#### Scenario: Pending video has protected media
- **WHEN** an authorized reviewer opens a current review task whose public media URL is absent
- **THEN** the page obtains short-lived review preview access and plays the video without making it anonymously readable

#### Scenario: Seeded evidence is present
- **WHEN** the task contains evidence from the reserved manual seed provider
- **THEN** the page labels it as test evidence and still displays bounded provider, model, policy, label, and confidence details
