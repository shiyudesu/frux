## ADDED Requirements

### Requirement: Private conversations and notifications have distinct message views
The shared Frux message destination SHALL distinguish user-authored private conversations from event-driven notifications while retaining the existing dark desktop shell, notification APIs, notification deep links, and typed routing.

#### Scenario: Existing messages route opens without a tab
- **WHEN** an authenticated user opens `/messages` without a conversation identifier
- **THEN** the existing notification view remains the backward-compatible default and the user can switch to the private-conversation view

#### Scenario: User opens a typed conversation route
- **WHEN** frontend code navigates to `/messages/{conversationId}` with a valid positive conversation ID
- **THEN** the message workspace activates the private-conversation view and loads that conversation through typed route parsing

#### Scenario: Invalid conversation route is supplied
- **WHEN** the conversation path identifier is missing, malformed, non-positive, or unsupported
- **THEN** route normalization enters a safe not-found or unselected message state without issuing an invalid chat request

#### Scenario: User returns to notifications
- **WHEN** the user switches from a private conversation to notifications
- **THEN** notification loading, pagination, mark-read, mark-all-read, actor rendering, and discussion or lifecycle navigation retain their existing behavior

### Requirement: Desktop chat workspace remains usable across Frux densities
The private-conversation view SHALL use a bounded conversation column, selected-conversation detail, scrollable history, composer, and truthful status presentation. It SHALL follow the existing wide, compact, and narrow desktop rules without adding mobile bottom navigation or globally scaling the shell.

#### Scenario: Wide desktop conversation opens
- **WHEN** a valid conversation opens at a viewport width of at least 1280px
- **THEN** the conversation list and selected history render as sibling regions with the composer visible and no horizontal document overflow

#### Scenario: Compact desktop conversation opens
- **WHEN** a valid conversation opens below 1280px
- **THEN** the 72px desktop rail remains in use and the interface may replace the list with detail plus an explicit back control rather than compressing both regions below their usable minimum

#### Scenario: Message history is loading or empty
- **WHEN** a selected conversation is loading or contains no messages
- **THEN** the detail region presents a specific loading or empty state without displaying notification-list placeholders

#### Scenario: Conversation becomes ineligible
- **WHEN** mutual follow is removed while an existing conversation is open
- **THEN** history remains readable, the composer becomes unavailable, and the interface explains that mutual follow is required to continue

#### Scenario: Shared video is no longer readable
- **WHEN** a video message resolves to the unavailable-video representation
- **THEN** the conversation preserves message ordering and renders a safe unavailable card without a playable URL or misleading navigation

### Requirement: Video share actions open a private-message recipient picker
Feed, video-detail, and collection-queue share actions SHALL open a consistent Frux-owned dialog that selects one eligible mutual-follow recipient and sends the active video as a private message.

#### Scenario: User activates share
- **WHEN** an authenticated user activates share on a readable video
- **THEN** the dialog identifies the active video and loads eligible recipients without leaving or pausing the current page solely to initialize the dialog

#### Scenario: User completes a share
- **WHEN** the selected recipient remains eligible and the video-message send succeeds
- **THEN** the dialog reports success, prevents duplicate submission, and offers an explicit conversation-navigation action

#### Scenario: User dismisses the picker
- **WHEN** the user closes the share dialog with Escape, its close control, or supported outside interaction
- **THEN** focus returns to the originating share action and no conversation or message is created

#### Scenario: User is not authenticated
- **WHEN** an unauthenticated visitor activates share
- **THEN** the interface routes to or presents the supported login action without displaying a false recipient picker
