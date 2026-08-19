# content-operations-console Specification

## Purpose

Defines the permission-aware administrative workspace for content review, video discovery, and audited enforcement operations.

## Requirements

### Requirement: Permission-Aware Admin Shell
The Web client SHALL provide an internal admin shell with a dedicated admin login and isolated admin session whose navigation and route affordances reflect the current server-confirmed admin permissions while treating backend authentication and authorization as authoritative.

#### Scenario: Reviewer opens the admin workspace
- **WHEN** the admin-authenticated principal has review permissions but not content-enforcement permission
- **THEN** review destinations are visible and content-enforcement controls are absent

#### Scenario: Visitor opens an admin route without admin authentication
- **WHEN** no valid admin session exists for a direct `/admin/*` navigation
- **THEN** the Web client routes to `/admin/login` rather than the consumer login or registration surface

#### Scenario: Consumer is already logged in
- **WHEN** a browser has a valid consumer session but no admin session and opens an admin route
- **THEN** the Web client still requires dedicated admin login and does not reuse the consumer token

#### Scenario: Stale client permission is rejected
- **WHEN** the client renders an action that the backend no longer authorizes
- **THEN** the Web client displays a stable forbidden state, clears affected cached admin data, and does not report success

#### Scenario: Admin authentication expires
- **WHEN** an admin API returns the authoritative admin-authentication 401
- **THEN** the shell clears only admin session data and returns to `/admin/login`

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

### Requirement: Video Processing Operations View
The video operations workspace SHALL provide a permission-protected processing view that presents
video-processing summaries, active work, terminal history, elapsed time, attempts, current step,
optional step progress, and plain-language failure reasons without requiring shell or database
access.

#### Scenario: Operator opens processing progress
- **WHEN** a principal with `content.enforce` opens the processing view
- **THEN** the page shows waiting, processing, failed, completed, and oldest-waiting summaries plus
  bounded active rows

#### Scenario: Processing row is displayed
- **WHEN** an active task has durable step progress
- **THEN** the page shows the video title, human-readable state and step, step percentage when known,
  elapsed time, attempts, and last update

#### Scenario: Step has no measurable percentage
- **WHEN** the current step is inspection or finalization
- **THEN** the page shows the step as active without inventing a percentage

#### Scenario: Operator expands diagnostics
- **WHEN** an operator opens a row's diagnostic details
- **THEN** the page may show technical identifiers, profile version, safe error code, and bounded
  diagnostic text but never credentials, tokens, object-storage keys, or lease owner

#### Scenario: Caller lacks content permission
- **WHEN** an authenticated principal without `content.enforce` requests processing operations
- **THEN** the API returns HTTP 403 and the Web client clears affected processing data

### Requirement: Adaptive Processing Refresh
The processing view SHALL refresh through bounded HTTP polling based on visible work and page
visibility.

#### Scenario: A video is processing
- **WHEN** at least one overview item is actively processing and the page is visible
- **THEN** the next overview refresh occurs after approximately five seconds

#### Scenario: Videos are waiting only
- **WHEN** work is waiting but none is actively processing and the page is visible
- **THEN** the next overview refresh occurs after approximately ten seconds

#### Scenario: No active work exists
- **WHEN** visible work is terminal and the page is visible
- **THEN** the next overview refresh occurs after approximately thirty seconds

#### Scenario: Browser tab is hidden
- **WHEN** the document becomes hidden
- **THEN** automatic refresh pauses and resumes with an immediate refresh when visible again

#### Scenario: Older request completes late
- **WHEN** a previous refresh returns after a newer refresh or route generation
- **THEN** the stale response cannot overwrite current processing state

### Requirement: Processing History Search
The processing view SHALL provide stable terminal-history pagination with filters for task outcome,
processing step, safe error code, video ID, and bounded completion time.

#### Scenario: Operator loads failure history
- **WHEN** failed tasks are requested with a valid cursor bound to unchanged filters
- **THEN** the API returns the next results in `completed_at DESC, id DESC` order without duplicates

#### Scenario: History filters change
- **WHEN** a cursor from one filter set is submitted with different filters
- **THEN** the API rejects the cursor instead of mixing histories

### Requirement: Audited Processing Retry Controls
The processing view SHALL allow operators with `content.enforce` to request single or bounded bulk
retry of eligible failed tasks using a registered reason, optional bounded note, explicit
confirmation, and idempotency protection.

#### Scenario: Operator retries one failed video
- **WHEN** the selected task is still eligible and retry commits
- **THEN** the page reports that the video has returned to the processing queue and refreshes
  immediately

#### Scenario: Operator retries selected failed videos
- **WHEN** no more than fifty selected task IDs are submitted
- **THEN** the API returns an explicit per-item result and the page does not hide partial conflicts
  or rejections

#### Scenario: Task changed before retry
- **WHEN** another process already requeued, completed, or deleted the selected task or source
- **THEN** retry returns a stable conflict and the page asks the operator to refresh

#### Scenario: Retry permission is revoked
- **WHEN** the retry API returns the authoritative permission denial
- **THEN** the page clears processing data and does not display optimistic success
