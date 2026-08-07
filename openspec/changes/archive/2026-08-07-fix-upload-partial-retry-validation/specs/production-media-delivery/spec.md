## ADDED Requirements

### Requirement: Actionable Upload Intent Validation
Production upload-session creation SHALL validate filename, media kind, content type, size, checksum, and idempotency metadata before issuing an object-storage request, and SHALL distinguish unsupported file constraints from upload-session state conflicts.

#### Scenario: Cover exceeds its size limit
- **WHEN** a client requests a cover upload session whose size exceeds the configured cover limit
- **THEN** Frux rejects the request with an actionable cover-size validation code and creates no upload session

#### Scenario: File type does not match its media kind
- **WHEN** a requested filename or content type is unsupported for the requested video or cover kind
- **THEN** Frux rejects the request with an actionable file-type validation code and creates no upload session

#### Scenario: Completed upload session is replayed
- **WHEN** a client retries the same owner, idempotency key, and upload fingerprint after that file completed
- **THEN** Frux returns the existing completed asset without requiring another object upload

#### Scenario: Paired upload retry uses independent keys
- **WHEN** a client retries only the failed member of a video and cover pair while reusing the completed member's original key
- **THEN** each upload session is resolved independently and the completed member remains reusable
