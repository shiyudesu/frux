## ADDED Requirements

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
