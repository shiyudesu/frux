## MODIFIED Requirements

### Requirement: Comment likes
Authenticated users SHALL be able to set or clear a like on any active visible root comment or reply. The operation SHALL be retry-safe, SHALL update the comment's like count exactly once per state transition, and SHALL return the viewer's effective state. The Web SHALL apply pending, confirmed, and rolled-back like state locally to the affected comment without resetting or repainting unrelated visible threads.

#### Scenario: User likes a comment
- **WHEN** an authenticated user sets an unliked active comment to liked
- **THEN** one active like fact is stored, the comment like count increases once, and the response reports `liked=true`

#### Scenario: User repeats the same like state
- **WHEN** the user repeats a like or unlike operation whose target state is already effective
- **THEN** the request succeeds without changing the counter or emitting another notification

#### Scenario: Anonymous viewer lists comments
- **WHEN** comments are listed without valid viewer authentication
- **THEN** public comments remain readable and every viewer-specific `liked` and delete-permission field is false

#### Scenario: Web updates one visible comment like
- **WHEN** a user likes or unlikes one visible root comment or reply
- **THEN** the target comment reflects the optimistic and effective state while unrelated comment cards, loaded pages, expanded replies, draft, focus, and scroll position remain stable

#### Scenario: Web rolls back a failed comment like
- **WHEN** a comment-like request fails after the optimistic update
- **THEN** only the target comment returns to its exact prior liked state and count, an actionable error is shown for that comment, and unrelated threads remain unchanged
