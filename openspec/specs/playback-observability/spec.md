# playback-observability Specification

## Purpose

Define privacy-safe, reliable playback telemetry and operational visibility without affecting media playback or breaking legacy QoS clients.

## Requirements

### Requirement: Versioned Playback Telemetry Batches
GCFeed SHALL accept bounded versioned playback telemetry batches with stable batch, playback-session, and event identifiers.

#### Scenario: Client submits a valid batch
- **WHEN** a client submits no more than the configured event and payload limits with supported event types
- **THEN** the API validates and stores the normalized events and returns an acceptance summary

#### Scenario: Client retries a batch
- **WHEN** previously accepted event IDs are submitted again
- **THEN** duplicate events are not stored or aggregated twice

#### Scenario: Batch exceeds limits
- **WHEN** event count, payload size, string length, or numeric bounds exceed the contract
- **THEN** the API rejects the batch without partial unbounded processing

### Requirement: Accurate Startup Measurement
The Web player SHALL measure source load start and first rendered frame using the best supported browser mechanism and SHALL identify the measurement method.

#### Scenario: Video frame callback is supported
- **WHEN** `requestVideoFrameCallback` is available and a frame is rendered
- **THEN** first-frame duration is measured from source load start to that callback

#### Scenario: Video frame callback is unavailable
- **WHEN** the browser lacks the frame callback API
- **THEN** the player uses the documented advancing-time or playing fallback and marks the fallback method

### Requirement: Rebuffer and Playback Quality Measurement
The Web player SHALL distinguish expected playback rebuffering from intentional pause and seek behavior and SHALL report bounded quality metrics.

#### Scenario: Playback stalls while expected to play
- **WHEN** the media enters waiting or stalled state during expected playback and later resumes
- **THEN** telemetry records one rebuffer interval with duration and recovery outcome

#### Scenario: User seeks
- **WHEN** buffering occurs as part of an intentional seek
- **THEN** it is classified as seek behavior rather than ordinary rebuffering

#### Scenario: Frame-quality API is available
- **WHEN** the browser exposes decoded and dropped frame counts
- **THEN** the terminal telemetry can include bounded frame-quality totals

### Requirement: Privacy-Safe Telemetry Context
Playback telemetry SHALL accept only documented normalized dimensions and SHALL NOT store full media URLs, signatures, tokens, cookies, titles, descriptions, or arbitrary client metadata.

#### Scenario: Client sends a signed media URL
- **WHEN** an event attempts to include a full signed media URL
- **THEN** ingestion rejects or strips the URL and retains at most the normalized CDN hostname and source type

#### Scenario: Metrics are exported
- **WHEN** telemetry updates Prometheus metrics
- **THEN** user IDs, video IDs, request IDs, and playback session IDs are not used as metric labels

### Requirement: Playback Metrics and Dashboards
The system SHALL expose low-cardinality playback startup, rebuffering, failure, source-selection, and telemetry-health metrics and SHALL visualize them in Grafana.

#### Scenario: Startup metrics are queried
- **WHEN** operators inspect the playback dashboard
- **THEN** p50, p95, and p99 startup or first-frame duration are available for supported scene, network, and player dimensions

#### Scenario: Playback error rate rises
- **WHEN** a configured minimum sample count and sustained error threshold are exceeded
- **THEN** a playback alert enters firing state and later resolves when the recovery condition is met

### Requirement: Telemetry Failure Isolation
Telemetry collection and delivery SHALL NOT block, pause, or fail media playback.

#### Scenario: Telemetry endpoint is unavailable
- **WHEN** a client flush fails
- **THEN** playback continues and the client performs only bounded retry with the same event IDs

#### Scenario: Page exits
- **WHEN** pending telemetry exists during page shutdown
- **THEN** the client attempts a bounded keepalive or beacon flush without delaying navigation

### Requirement: Legacy QoS Compatibility
The existing playback QoS endpoint SHALL remain available while supported clients migrate to the expanded telemetry contract.

#### Scenario: Legacy QoS client reports
- **WHEN** a client submits the existing first-frame, stutter-count, and watch-duration payload
- **THEN** the server continues to validate and persist it using the existing response contract
