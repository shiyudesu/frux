## ADDED Requirements

### Requirement: Non-billable validation is the default
The acceptance runner SHALL default to a validation-only mode that makes no upload, review, video,
embedding, Similar, Hybrid, or external-model request. Billable execution SHALL require both an
explicit command option and an independent environment acknowledgement.

#### Scenario: Runner starts without execution confirmation
- **WHEN** the operator invokes the runner without both billable-execution gates
- **THEN** it validates configuration and fixture inputs, reports the planned stages and maximum model calls, and makes no state-changing or model request

#### Scenario: Only one execution gate is present
- **WHEN** either the command option or environment acknowledgement is missing
- **THEN** the runner remains non-billable and does not partially execute the acceptance workflow

### Requirement: Runtime prerequisites are verified before fixture creation
The runner SHALL verify bounded API and Adapter health, metrics availability, PostgreSQL read access,
S3 direct-upload mode, selected multimodal runtime availability, fixture validity, and required
acceptance credentials before creating a video.

#### Scenario: Required runtime is ready
- **WHEN** all required services, credentials, S3 upload mode, feature paths, and fixture files pass validation
- **THEN** the runner may enter the explicitly confirmed execution workflow

#### Scenario: A prerequisite is missing
- **WHEN** a service, credential, fixture, database connection, provider path, or required feature is unavailable
- **THEN** the runner stops with a closed prerequisite code and does not create a fixture video

### Requirement: Acceptance uses normal product workflows
The runner SHALL use existing authenticated login, upload-session, presigned upload, asset completion,
video creation, human review claim/decision, Similar Videos, and Hybrid Search interfaces. It SHALL
observe PostgreSQL but SHALL NOT mutate roles, review state, jobs, facts, projections, or vectors
directly.

#### Scenario: Two fixture videos complete acceptance
- **WHEN** execution is confirmed and the configured runtime remains healthy
- **THEN** two S3-backed public videos pass review, complete exact-contract multimodal jobs, produce matching facts and projections, participate in Similar retrieval, and answer one Hybrid query

#### Scenario: Review credentials lack permission
- **WHEN** the configured admin or reviewer account cannot claim and approve the fixture cases
- **THEN** the run fails at the review stage without changing account roles or bypassing authorization

### Requirement: Every stage is bounded and classified
The runner SHALL execute a fixed stage machine with bounded HTTP bodies, non-redirecting authenticated
requests, polling intervals, per-stage deadlines, and closed failure codes. It SHALL stop dependent
stages after a failure and SHALL NOT start detached retries or work.

#### Scenario: A job does not finish before its deadline
- **WHEN** a fixture multimodal job remains pending, leased, or retryable past the configured deadline
- **THEN** the runner reports a timeout with the last bounded job state and performs no retrieval stage for that fixture

#### Scenario: Provider or API fails temporarily
- **WHEN** a bounded request returns a retryable service failure
- **THEN** the runner records the closed stage failure and exits without an unbounded local retry loop

### Requirement: Technical vector and retrieval evidence is verified
The runner SHALL verify the active contract, successful job state, attempt count, vector dimension,
finite unit norm, vector digest equality between fact and projection, Similar availability/result IDs,
Hybrid result IDs, and bounded provider operation/token deltas.

#### Scenario: Vector evidence is compatible
- **WHEN** both fixture jobs succeed under the selected contract
- **THEN** the report shows the expected dimension, unit-norm tolerance, matching fact/projection digests, and exact contract identity without including vector components

#### Scenario: Metrics reset during the run
- **WHEN** a provider counter after execution is lower than its baseline
- **THEN** the report marks that metric delta unavailable and does not emit a negative or fabricated value

### Requirement: Secrets and sensitive payloads never enter output
Acceptance credentials and runtime secrets SHALL be read only from environment variables. Command
arguments, stdout, stderr, report files, and normal logs SHALL NOT contain passwords, bearer tokens,
API keys, HMAC secrets, PostgreSQL credentials, presigned URLs, raw provider bodies, raw vectors, or
media bytes.

#### Scenario: Acceptance succeeds
- **WHEN** the runner emits its final report
- **THEN** the report contains only bounded identifiers, contract fields, closed results, durations, counts, token deltas, and retrieval IDs

#### Scenario: A secret-bearing request fails
- **WHEN** login, upload, review, provider, or database access returns an error
- **THEN** the runner emits a closed failure code without copying the request, credential, URL query, or raw response body

### Requirement: Reports are versioned and fixture cleanup is narrow
The runner SHALL emit a versioned `technical_acceptance` JSON report. Fixtures SHALL be retained by
default with their created identifiers reported. Optional cleanup SHALL delete only the two videos
created by the current run through the authenticated video API after retrieval verification.

#### Scenario: Default execution completes
- **WHEN** acceptance succeeds without the cleanup option
- **THEN** the report marks fixtures retained and lists the current run's video and asset identifiers for later inspection or Golden Set use

#### Scenario: Cleanup is requested
- **WHEN** acceptance completes and the operator requested cleanup
- **THEN** the runner deletes only the current run's two videos through normal authorization and reports cleanup success or failure without deleting accounts, media objects directly, historical facts, or previous fixtures
