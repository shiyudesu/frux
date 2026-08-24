## MODIFIED Requirements

### Requirement: Versioned authenticated multimodal provider protocol
Frux SHALL invoke external multimodal inference through a versioned HTTP protocol that defines
readiness, public-video embedding, and public-query embedding operations. Every request and response
SHALL be bound to an operation ID and authenticated over its exact body. Non-local endpoints MUST use
HTTPS; local/test MAY use HTTP only for loopback or the exact private Compose hostname
`multimodal-provider` when explicitly allowed.

#### Scenario: Video embedding request is sent
- **WHEN** the Worker submits validated canonical public text and prepared bounded images
- **THEN** the provider receives no storage URL, credential, user identity, behavior data, or arbitrary metadata and the request is authenticated with the configured protocol version

#### Scenario: Query embedding request is sent
- **WHEN** the API submits a validated canonical public search query
- **THEN** the provider receives only the query, requested contract, source hash, protocol envelope, and no authenticated-user or request-session identity

#### Scenario: Remote endpoint uses insecure transport
- **WHEN** configuration enables an HTTP endpoint that is neither loopback nor the exact allowed local Compose hostname
- **THEN** Frux rejects configuration before making a provider request

#### Scenario: Production uses the Compose hostname over HTTP
- **WHEN** staging or production configuration points to `http://multimodal-provider`
- **THEN** Frux rejects configuration before process startup

#### Scenario: Provider redirects a signed request
- **WHEN** an embedding or readiness endpoint returns a redirect
- **THEN** Frux rejects the response and does not forward the signed request to the redirect target

## ADDED Requirements

### Requirement: Billable development provider composition is explicit and secret-scoped
Frux SHALL provide an optional development Compose overlay that starts the Adapter and enables Worker
video-job execution. The upstream API key MUST be present only in the Adapter container; Worker SHALL
receive only the selected profile, internal endpoint, and Frux HMAC, and API SHALL remain independent
from upstream credentials.

#### Scenario: Multimodal overlay is selected
- **WHEN** Compose loads the multimodal overlay and configured repository-local environment
- **THEN** Adapter starts with the upstream key, Worker waits for Adapter health and validates signed video readiness, and API does not receive the upstream key

#### Scenario: Base Compose is selected
- **WHEN** the overlay is absent
- **THEN** Adapter is not started, video jobs remain disabled, and Session Semantic continues using already persisted vectors

#### Scenario: Required model credentials are absent
- **WHEN** the overlay is selected without a configured API key or HMAC
- **THEN** configuration or Adapter startup fails before Worker claims a video job
