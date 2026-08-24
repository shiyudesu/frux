## MODIFIED Requirements

### Requirement: Versioned authenticated multimodal provider protocol
Frux SHALL invoke external multimodal inference through a versioned HTTP protocol that defines
readiness, public-video embedding, and public-query embedding operations. Every request and response
SHALL be bound to an operation ID and authenticated over its exact body. Non-local endpoints MUST use
HTTPS. Local/test MAY use HTTP for loopback or the exact private Compose hostname
`multimodal-provider`; staging/production MAY use that exact hostname only with the explicit private-
network authorization and MUST reject every other plaintext host.

#### Scenario: Video embedding request is sent
- **WHEN** the Worker submits validated canonical public text and prepared bounded images
- **THEN** the provider receives no storage URL, credential, user identity, behavior data, or arbitrary metadata and the request is authenticated with the configured protocol version

#### Scenario: Query embedding request is sent
- **WHEN** the API submits a validated canonical public search query
- **THEN** the provider receives only the query, requested contract, source hash, protocol envelope, and no authenticated-user or request-session identity

#### Scenario: Remote endpoint uses insecure transport
- **WHEN** configuration enables an HTTP endpoint that is neither loopback nor the exact authorized private Compose hostname
- **THEN** Frux rejects configuration before making a provider request

#### Scenario: Production private Adapter is explicitly authorized
- **WHEN** production Worker uses `http://multimodal-provider:8099` with private-network authorization
- **THEN** configuration accepts only that endpoint and retains signed request/response validation

#### Scenario: Provider redirects a signed request
- **WHEN** an embedding or readiness endpoint returns a redirect
- **THEN** Frux rejects the response and does not forward the signed request to the redirect target

## ADDED Requirements

### Requirement: Production multimodal credentials are process-scoped
Production composition SHALL provide the upstream API key only to Adapter. API MUST receive neither
the upstream key nor video-provider endpoint/HMAC, and Worker MUST receive no upstream key.

#### Scenario: Production container environments are rendered
- **WHEN** the multimodal profile is enabled
- **THEN** only Adapter contains `DASHSCOPE_API_KEY`, Worker contains the private endpoint/HMAC, and API contains only Session runtime/profile flags

#### Scenario: Multimodal profile is disabled
- **WHEN** production deployment omits the explicit profile gate
- **THEN** Adapter is absent and API/Worker start with multimodal features disabled
