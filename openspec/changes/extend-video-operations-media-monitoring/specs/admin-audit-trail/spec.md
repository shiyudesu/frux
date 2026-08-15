## ADDED Requirements

### Requirement: Audited Media Processing Retry
Frux SHALL register immutable `media_processing.retry` audit facts using `content.enforce` and the
`media_processing_job` target type. Every successful retry SHALL commit its audit fact in the same
PostgreSQL transaction as the retry state and idempotency result.

#### Scenario: Single retry commits
- **WHEN** an authorized operator successfully retries one failed processing task
- **THEN** one success fact records actor, target job ID, video ID, registered reason, previous and
  new state, previous attempt count, route, method, request ID, and idempotency-key hash

#### Scenario: Bulk retry has partial success
- **WHEN** a bounded bulk request succeeds for some selected jobs
- **THEN** each successful job has its own audit fact and failed items have no success fact

#### Scenario: Audit insertion fails
- **WHEN** retry state changes are valid but the success audit fact cannot be inserted
- **THEN** that job's retry, idempotency result, and retry-notification outbox entry roll back and the
  API does not report that item as successful

#### Scenario: Retry audit detail is inspected
- **WHEN** an authorized audit reader views a media retry fact
- **THEN** detail contains no video title, filename, object key, raw diagnostic, operator note,
  credential, token, or lease owner

#### Scenario: Unauthorized retry is attempted
- **WHEN** an authenticated principal without `content.enforce` calls a retry endpoint
- **THEN** Frux records the bounded denied attempt according to the existing denied-attempt policy
  and returns HTTP 403
