## ADDED Requirements

### Requirement: Bounded Native Media Cache Reuse
The Web player SHALL use stable generation-based public media URLs and normal browser HTTP caching
within the server-declared revocation window without adding unbounded Service Worker, Cache Storage,
IndexedDB, or filesystem video persistence.

#### Scenario: Same public video is replayed shortly
- **WHEN** the same generation URL is requested again while its redirect and media response remain
  fresh
- **THEN** the browser may reuse cached redirect, ETag, and byte ranges without changing player state

#### Scenario: Exposure generation changes
- **WHEN** a restored video receives a new generation URL
- **THEN** the player treats it as a new source revision and does not reuse stale media state from the
  previous generation

#### Scenario: Cache entry reaches revocation bound
- **WHEN** the public media cache lifetime reaches 30 minutes
- **THEN** the browser must revalidate before continuing to rely on the cached response

#### Scenario: Protected preview is played
- **WHEN** owner or reviewer media uses a protected access URL
- **THEN** the player receives `private, no-store` behavior and does not persist it as public media
  cache

#### Scenario: Browser does not reuse Range cache
- **WHEN** a browser chooses not to reuse cached partial video responses
- **THEN** playback remains correct and falls back to a normal bounded signed request without
  claiming a guaranteed cache hit
