## ADDED Requirements

### Requirement: Direct Deterministic Output Publication
A media-processing attempt SHALL publish a completed local output directly to its deterministic
final protected object key and SHALL NOT upload, download, or copy an object-store temporary body.

#### Scenario: Final key is absent
- **WHEN** processing has a valid local output and the deterministic key does not exist
- **THEN** Worker performs one PUT from the local file and verifies final size and checksum before
  PostgreSQL finalization

#### Scenario: Final key already matches
- **WHEN** a retry observes the expected final size and checksum
- **THEN** Worker reuses the existing output without transferring the body again

#### Scenario: Final key conflicts
- **WHEN** the deterministic key exists with unexpected metadata
- **THEN** Worker records an explicit retryable or terminal failure and does not overwrite the
  conflicting file

#### Scenario: Database finalization is interrupted
- **WHEN** object PUT succeeds but fenced PostgreSQL finalization does not
- **THEN** the unreferenced protected object is left for delayed orphan reconciliation and is never
  advertised as ready

#### Scenario: Attempt is reclaimed during PUT
- **WHEN** the job lease is lost while final output is being uploaded
- **THEN** the stale attempt cannot finalize the job, and any unreferenced deterministic object is
  handled by reconciliation
