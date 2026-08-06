## ADDED Requirements

### Requirement: Permission-Aware Admin Shell
The Web client SHALL provide an internal admin shell whose navigation and route affordances reflect the authenticated principal's admin permissions while treating backend authorization as authoritative.

#### Scenario: Reviewer opens the admin workspace
- **WHEN** the session principal has review permissions but not content-enforcement permission
- **THEN** review destinations are visible and content-enforcement controls are absent

#### Scenario: Stale client permission is rejected
- **WHEN** the client renders an action that the backend no longer authorizes
- **THEN** the Web client displays a stable forbidden state and does not report success

### Requirement: Review Operations Workspace
The admin shell SHALL provide review queue and case-detail views with evidence, assignment, lease renewal, approve, and reject actions and truthful asynchronous states.

#### Scenario: Reviewer decides a leased case
- **WHEN** the reviewer has a valid lease and submits a valid decision
- **THEN** the page displays the committed final state and removes the case from active work

#### Scenario: Lease expires during review
- **WHEN** a decision returns the stable lease-expired conflict
- **THEN** the page preserves the inspected evidence, disables stale actions, and offers a fresh queue reload

### Requirement: Admin Video Search
Authorized operators SHALL be able to search videos by lifecycle status, author, video identifier, keyword, and bounded creation window using stable cursor pagination.

#### Scenario: Operator filters rejected videos
- **WHEN** an authorized operator selects the rejected status
- **THEN** the result contains only matching non-deleted videos in `created_at DESC, id DESC` order

#### Scenario: Cursor filters change
- **WHEN** a cursor created for one filter set is submitted with different filters
- **THEN** the API rejects the cursor instead of mixing result sets

### Requirement: Reasoned Content Enforcement
An operator with `content.enforce` SHALL be able to take an eligible video offline or restore an approved offline video only with a registered reason code, optional bounded note, expected version, and audit attribution.

#### Scenario: Operator takes published video offline
- **WHEN** an authorized operator confirms a valid takedown against the current version
- **THEN** the video becomes offline, public caches are invalidated, and the action is audited

#### Scenario: Enforcement side effect temporarily fails
- **WHEN** cache invalidation or media protection/publication fails after the transition commits
- **THEN** a bounded Worker retains and retries the transactional intent and marks it delivered only after all side effects succeed

#### Scenario: Operator restores unapproved video
- **WHEN** an operator attempts to restore a rejected or never-approved video
- **THEN** Frux rejects the transition and the Web client displays the domain error
