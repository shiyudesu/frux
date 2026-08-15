## MODIFIED Requirements

### Requirement: Versioned Media Processing
Frux SHALL process each source asset with an idempotent versioned media profile, SHALL persist the
state and metadata of every generated output, and SHALL create a durable creator notification only
when the required current baseline reaches a terminal failure. New processing SHALL produce exactly
one browser-compatible MP4 baseline at the source resolution, except for the minimal even-dimension
adjustment required by H.264, and SHALL NOT generate selectable renditions or DASH outputs.
Input-duration policy, per-command timeout, and encoder speed preset SHALL remain explicit validated
runtime configuration.

#### Scenario: Browser-compatible source is uploaded
- **WHEN** the primary source video is H.264 and its optional audio is AAC
- **THEN** the worker stream-copies the selected streams into one fast-start baseline MP4 without
  changing the source resolution

#### Scenario: Only source audio requires normalization
- **WHEN** the primary video is H.264 but its audio is not AAC
- **THEN** the worker copies video, encodes audio once to AAC, and produces one source-resolution
  baseline MP4

#### Scenario: Source video requires normalization
- **WHEN** a valid uploaded source uses another accepted video codec
- **THEN** the worker performs one H.264/AAC normalization pass at the source resolution before
  public eligibility

#### Scenario: Source dimensions are odd
- **WHEN** H.264 encoding cannot retain an odd source width or height
- **THEN** the worker floors only the affected dimensions to the nearest positive even value

#### Scenario: Processing output is exposed
- **WHEN** the single baseline completes and the video's other public gates are satisfied
- **THEN** `media_url` and ordered playback sources expose the same MP4, no new DASH source is
  returned, and the player shows no quality selector for that single source

#### Scenario: Existing completed adaptive media is read
- **WHEN** a previously completed video already has rendition or DASH variants
- **THEN** those immutable outputs remain readable and are not reprocessed or deleted by this change

#### Scenario: Unfinished legacy-profile job is reclaimed
- **WHEN** a pending or retryable v1 job has no committed ready baseline
- **THEN** the current worker may finish it using the single source-resolution baseline behavior
  without creating duplicate concurrent output

#### Scenario: Source exceeds configured duration
- **WHEN** probing finds a source duration greater than the configured processing limit
- **THEN** the job fails terminally with the stable `duration_limit` reason

#### Scenario: Processing job is delivered twice
- **WHEN** the same asset and processing-profile version is consumed more than once
- **THEN** output publication, database records, and terminal-failure notifications remain idempotent

#### Scenario: Processing fails retryably
- **WHEN** probing, remuxing, transcoding, checksum validation, or object publication fails with
  attempts remaining
- **THEN** the asset records a retryable failure without advertising incomplete output or notifying
  the creator of a terminal failure

#### Scenario: Processing fails terminally
- **WHEN** the required baseline exhausts bounded retries or encounters a registered non-retryable failure
- **THEN** the asset records terminal failure and commits one creator notification fact containing only a safe reason code
