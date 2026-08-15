## MODIFIED Requirements

### Requirement: Versioned Media Processing
Frux SHALL process each source asset with an idempotent versioned media profile, SHALL persist the
state and metadata of every generated output, and SHALL create a durable creator notification only
when the required current baseline reaches a terminal failure. Input-duration policy, per-command
timeout, and encoder speed preset SHALL be explicit validated runtime configuration, and the command
budget SHALL not deterministically reject a source that is within the configured duration policy
solely because of the previous fixed 15-minute limit.

#### Scenario: Source requires normalization
- **WHEN** a valid uploaded source uses an accepted codec or layout that is not the required browser baseline
- **THEN** the worker generates a browser-compatible baseline MP4 before public eligibility

#### Scenario: Rendition ladder is generated
- **WHEN** the source resolution supports multiple configured renditions
- **THEN** the worker generates only non-upscaled bounded renditions and an adaptive manifest that references verified immutable outputs

#### Scenario: Accepted long source processes within configured budget
- **WHEN** a source is within the configured duration limit but one rendition requires more than 15
  minutes on the production host
- **THEN** ffmpeg continues up to the configured command timeout with lease heartbeats remaining
  active

#### Scenario: Source exceeds configured duration
- **WHEN** probing finds a source duration greater than the configured processing limit
- **THEN** the job fails terminally with the stable `duration_limit` reason

#### Scenario: Invalid processing runtime configuration
- **WHEN** command timeout, maximum duration, or encoder preset is empty, out of bounds, or unsupported
- **THEN** API and Worker startup fail explicitly before accepting or claiming media work

#### Scenario: Multiple renditions include audio
- **WHEN** DASH packaging receives multiple verified MP4 renditions and an optional audio stream
- **THEN** ffmpeg maps the intended video and audio streams into valid adaptation sets and the worker
  verifies the generated manifest bundle

#### Scenario: Processing job is delivered twice
- **WHEN** the same asset and processing-profile version are consumed more than once
- **THEN** output publication, database records, and terminal-failure notifications remain idempotent

#### Scenario: Processing fails retryably
- **WHEN** probing, transcoding, checksum validation, or object publication fails with attempts remaining
- **THEN** the asset records a retryable failure without advertising incomplete outputs or notifying the creator of a terminal failure

#### Scenario: Processing fails terminally
- **WHEN** the required current baseline exhausts bounded retries or encounters a registered non-retryable failure
- **THEN** the asset records terminal failure and commits one creator notification fact containing only a safe reason code
