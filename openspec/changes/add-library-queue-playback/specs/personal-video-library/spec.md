## ADDED Requirements

### Requirement: Library queue source state
The Web SHALL expose each authenticated personal-library tab as an ordered queue source with independent items, cursor, has-more, loading, error, and mutation state. Opening or closing playback MUST NOT destroy already loaded pages, and appended pages MUST preserve the source API order.

#### Scenario: User returns from collection playback
- **WHEN** the user closes a Likes, Favorites, Watch History, or Watch Later queue
- **THEN** the corresponding library tab retains its loaded items, cursor, errors, and scroll position

#### Scenario: Queue pagination finishes after another tab becomes active
- **WHEN** a page request for one library source resolves after the profile switches to another tab
- **THEN** the response updates only its owning source and does not replace the active tab's items

## MODIFIED Requirements

### Requirement: Watch Later State
GCFeed SHALL allow an authenticated user to idempotently add or remove a readable video from Watch Later and list active Watch Later entries using stable cursor pagination. The Web SHALL expose a functional Watch Later add action from supported playback surfaces and a remove action from the Watch Later library and queue.

#### Scenario: User adds video to Watch Later
- **WHEN** the user sends PUT for a readable video not currently active in Watch Later
- **THEN** an active Watch Later fact is stored and returned

#### Scenario: User repeats Watch Later mutation
- **WHEN** the same active state is requested repeatedly
- **THEN** the API returns the current state without creating duplicate active facts

#### Scenario: User lists Watch Later
- **WHEN** the user requests Watch Later
- **THEN** active readable videos are returned by `updated_at DESC, video_id DESC`

#### Scenario: Unreadable Watch Later candidates lead a page
- **WHEN** newer active Watch Later facts reference unreadable videos but older readable facts exist
- **THEN** the service replenishes for at most the configured round bound and returns a stable next cursor with `has_more=true` when older candidates remain

#### Scenario: User adds the active playback item
- **WHEN** an authenticated user activates “稍后再看” for a readable video outside the Watch Later source
- **THEN** the Web submits the idempotent PUT once, disables duplicate submission while pending, and reports success or an actionable failure

#### Scenario: User removes the active Watch Later item
- **WHEN** the user removes the current item from a Watch Later queue
- **THEN** the source removes only that video and the queue selects the next item, the previous item, or a truthful empty state without restoring unrelated removed items
