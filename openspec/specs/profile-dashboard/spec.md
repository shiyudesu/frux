# profile-dashboard Specification

## Purpose

Defines aggregated profile data, atomic profile and privacy editing, logout asset ordering, dashboard behavior, truthful tabs, public privacy, and request-state isolation.

## Requirements

### Requirement: Aggregated Profile Summary
Frux SHALL return an authenticated profile summary containing the owner's display profile, account identifier, gender, following count, follower count, public work count, received-like count, and profile privacy settings. Public profile responses SHALL omit the account identifier and contain only fields allowed for public display.

#### Scenario: Current user opens profile
- **WHEN** an authenticated user requests their profile
- **THEN** the response includes their own account identifier, display fields, relationship counts, public work count, received-like count, gender, and profile settings

#### Scenario: Visitor opens public profile
- **WHEN** a visitor requests another user's public profile
- **THEN** the response omits the account identifier and other private account data and returns only public profile statistics and settings-derived capabilities

#### Scenario: Visitor profile renders identity
- **WHEN** the Web renders another user's public profile
- **THEN** it displays nickname-based public identity without an account row or account fallback

### Requirement: Public Profile Cache Privacy
The Web SHALL NOT persist another user's account identifier in the public-profile cache and SHALL sanitize legacy cache entries before using them.

#### Scenario: Public profile is cached
- **WHEN** the Web caches profile data used to navigate to or render another user's profile
- **THEN** the stored entry contains the user ID and public presentation fields without an account identifier

#### Scenario: Legacy account-bearing cache is read
- **WHEN** the Web reads a public-profile cache created before account identifiers became private
- **THEN** it ignores and removes the cached account field before the entry can be rendered or retained

### Requirement: Profile Editing
Frux SHALL allow an authenticated user to update nickname, avatar URL, bio, gender, and optional profile privacy through the existing profile update boundary while preserving validation and authentication behavior. Profile and privacy changes in one request SHALL commit atomically, update only supplied columns, and return the complete current profile.

#### Scenario: User saves valid profile changes
- **WHEN** the authenticated user submits valid nickname, avatar, bio, or gender changes
- **THEN** the profile is updated and the next profile response reflects the saved values

#### Scenario: User submits invalid gender
- **WHEN** profile editing receives a gender value outside the supported domain enumeration
- **THEN** the API rejects the request without changing the stored profile

#### Scenario: Combined profile save fails
- **WHEN** either the profile write or privacy-setting write fails during a combined save
- **THEN** neither change is committed and the client retains the prior session profile

#### Scenario: Concurrent partial profile saves
- **WHEN** concurrent requests update different profile or privacy fields from stale snapshots
- **THEN** every supplied field is committed without overwriting unrelated fields changed by the other request

### Requirement: Profile Privacy Settings
Frux SHALL provide authenticated read and partial-update operations for compatibility, with privacy-preserving defaults. Only liked videos MAY be publicly exposed; favorites SHALL remain owner-only regardless of the compatibility field.

#### Scenario: New user has default settings
- **WHEN** a user has no explicit profile settings row
- **THEN** liked and favorited video visibility resolves to private

#### Scenario: User changes liked-video visibility
- **WHEN** the user sets liked-video visibility to public
- **THEN** their public profile may expose the liked-videos tab and public liked-video list

#### Scenario: Web edits favorite visibility
- **WHEN** the profile editor renders privacy controls
- **THEN** favorites are described as owner-only and no public favorite control or claim is rendered

### Requirement: Logout Asset Cookie Ordering
Frux SHALL make Web local authentication and upload-active marker removal authoritative on logout. Cookie-based upload identity SHALL require both the HttpOnly asset token and the active marker. The Web client SHALL clear local authentication and the marker before attempting logout. Because JWT logout is stateless, the logout response SHALL NOT mutate either asset Cookie, so a stale logout response cannot clear a newer login. Ordinary authenticated responses SHALL NOT refresh the asset token.

#### Scenario: Expired session logs out
- **WHEN** the client deletes the current session with no valid access token
- **THEN** the Web has already removed local authentication and the active marker, and the server returns success without setting Cookie headers

#### Scenario: Logout request fails
- **WHEN** the Web logout request fails
- **THEN** local authentication remains cleared and cookie-based private asset access stays disabled

#### Scenario: Stale logout finishes after newer login
- **WHEN** an older logout response arrives after a newer login has written an asset token and activated the Web marker
- **THEN** the logout response has no Cookie mutation and cannot clear the newer login

#### Scenario: Authenticated response finishes after logout
- **WHEN** an older authenticated response completes after local logout
- **THEN** it sets no asset credential cookie and cannot reactivate cookie-based private asset access

### Requirement: Douyin-Style Desktop Profile Dashboard
The Web profile route SHALL render a full-width banner header, circular avatar, inline statistics, profile actions, primary content tabs, context-specific secondary controls, and dense portrait content grids within the existing Frux shell.

#### Scenario: Wide desktop profile renders
- **WHEN** the authenticated profile opens at a viewport width of at least 1280px
- **THEN** it uses the 160px rail and 56px header with a full-width profile banner, inline counts, primary tabs, work controls, and a six-column-capable portrait grid without a rounded outer profile card

#### Scenario: Compact desktop profile renders
- **WHEN** the profile opens between 901px and 1279px
- **THEN** the 72px compact rail remains visible and profile controls reflow without horizontal page overflow

### Requirement: Truthful Profile Tabs
The authenticated profile SHALL expose Works, Likes, Favorites, Watch History, and Watch Later tabs backed by real data. The Works tab SHALL expose Published, Private Works, and Collections secondary views. Recommend, Short Drama, and Appointments tabs MUST NOT be rendered on the personal profile.

#### Scenario: User changes primary tab
- **WHEN** the user activates a supported primary tab
- **THEN** the page loads that tab's real data and presents loading, error, empty, and ready states

#### Scenario: Recommendation remains outside the personal profile
- **WHEN** the authenticated profile navigation is rendered
- **THEN** no Recommend tab is present and recommendation discovery remains available through the recommendation Feed route

#### Scenario: Unsupported Douyin domains are absent
- **WHEN** the profile navigation is rendered
- **THEN** no Short Drama or Appointments tab or cosmetic-only control is present

### Requirement: Public Profile Privacy Enforcement
The public profile SHALL display public works and public collections, MAY display liked videos only when permitted, and MUST NOT display private works, favorites, watch history, watch later, batch controls, or profile editing.

#### Scenario: Liked videos are private
- **WHEN** a visitor opens a user whose liked-video visibility is private
- **THEN** the public profile does not return or render that user's liked-video items

#### Scenario: Owner-only controls remain hidden
- **WHEN** a visitor opens another user's profile
- **THEN** profile editing, private works, personal library tabs, and batch management are absent

### Requirement: Profile State Isolation
The Web client SHALL maintain independent loading, error, items, cursor, and has-more state for each profile tab so that switching tabs does not destroy previously loaded pages.

#### Scenario: User returns to a loaded tab
- **WHEN** the user loads a page of liked videos, switches to another tab, and returns
- **THEN** the previously loaded liked-video items remain available without being replaced by another tab's state

#### Scenario: Public profile target changes
- **WHEN** the routed public-profile `userId` changes while old requests are still pending
- **THEN** all target-specific state resets immediately and stale responses cannot update the new profile

#### Scenario: Public follow state loads directly
- **WHEN** an authenticated visitor opens another user's public profile
- **THEN** the Web reads that one relationship directly without scanning a bounded following list

#### Scenario: Follow mutation overtakes relationship read
- **WHEN** a pending relationship-state read finishes after a successful follow or unfollow mutation
- **THEN** the stale read cannot overwrite the mutation result

#### Scenario: Relation modal tab changes during pagination
- **WHEN** a following or follower page resolves after the user switches tabs or closes the modal
- **THEN** that page cannot populate the active tab or append after reset

#### Scenario: Concurrent Watch Later removal fails
- **WHEN** multiple optimistic removals overlap and one request fails
- **THEN** only the failed video is restored without replacing other successful list changes

#### Scenario: Collection editor reaches unloaded works
- **WHEN** a creator manages a collection with more works than the already loaded profile pages
- **THEN** the editor provides independent search and pagination across public and private creator works

#### Scenario: Collection create retry
- **WHEN** collection creation fails and the user retries the same normalized payload
- **THEN** the Web client reuses its idempotency key, and rotates the key after success or a payload change

#### Scenario: Profile content statistics refresh after mutations
- **WHEN** the owner completes a batch visibility/delete action or creates/deletes a collection
- **THEN** the Web refetches the current profile through a stable race-safe callback and immediately updates the session-backed work and collection statistics

### Requirement: Public profiles expose truthful private-message eligibility
An authenticated visitor viewing another user's public profile SHALL receive an independently loaded private-message eligibility state. The profile SHALL expose a private-message action only with truthful mutual-follow guidance and SHALL open the canonical conversation when messaging is eligible.

#### Scenario: Mutually following visitor opens a public profile
- **WHEN** the authenticated visitor and profile owner currently follow one another
- **THEN** the profile presents an enabled private-message action that resolves and opens their canonical conversation

#### Scenario: Visitor is not mutually following
- **WHEN** either follow direction is inactive
- **THEN** the profile does not present an enabled send action and explains that mutual follow is required

#### Scenario: User views their own public profile
- **WHEN** the authenticated user opens their own public-profile route
- **THEN** no self-message action is rendered and the existing own-profile navigation remains available

#### Scenario: Routed profile changes during eligibility loading
- **WHEN** an eligibility response for one user arrives after the routed public-profile user ID changes
- **THEN** the stale response cannot enable, disable, or navigate the new profile's private-message action

#### Scenario: Eligibility changes before conversation creation
- **WHEN** the profile showed eligibility but mutual follow is removed before the visitor activates the action
- **THEN** the authoritative create-conversation request fails safely and the profile refreshes to an ineligible state
### Requirement: Douyin-Style Creator Month Archive Filter
The authenticated profile Works toolbar SHALL replace browser-native date inputs with a Frux-owned month archive trigger and two-column selector. The trigger SHALL display a calendar icon, `日期筛选` when no month is active, the localized selected year and month when filtered, and a disclosure indicator. The panel SHALL present `全部` and available years in descending order in its first column and the active year's available months in its second column.

#### Scenario: Creator opens an unfiltered work tab
- **WHEN** the public or private Works tab is active with no selected month
- **THEN** the toolbar displays `日期筛选` and the archive panel identifies `全部` as selected

#### Scenario: Creator explores a year
- **WHEN** the creator hovers, focuses, or keyboard-navigates to an available year
- **THEN** the second column displays only the available months for that year without issuing a video query

#### Scenario: Creator selects a month
- **WHEN** the creator activates an available month
- **THEN** the panel closes, the trigger displays `YYYY年M月`, the active tab's pagination resets, and the complete current keyword and month filter is applied immediately

#### Scenario: Creator selects a year
- **WHEN** the creator activates an available year from the first column
- **THEN** the first available month for that year is selected and applied

#### Scenario: Creator clears the filter
- **WHEN** the creator activates `全部`
- **THEN** the selected month clears, the trigger returns to `日期筛选`, and the active tab reloads from its first page without creation-date bounds

#### Scenario: Creator switches work visibility
- **WHEN** the creator changes between public and private Works tabs
- **THEN** each tab restores its own selected month, archive metadata, keyword draft, loaded items, cursor, and error state

### Requirement: Creator Month Archive State Integrity
The Web SHALL load archive metadata independently for public and private Works tabs, expose loading and explicit retryable error states, and refresh affected archive metadata after creator mutations. It MUST NOT infer a complete archive from only the currently loaded creator page.

#### Scenario: Archive months are loading
- **WHEN** the active tab's archive request is pending
- **THEN** the date control exposes a truthful loading state and does not present an incomplete list derived from visible cards

#### Scenario: Archive loading fails
- **WHEN** the archive-month request fails
- **THEN** the video grid retains its own valid state while the date control presents an explicit failure and retry action

#### Scenario: Batch mutation removes the selected month
- **WHEN** a successful delete or visibility mutation removes the final matching work from the active tab's selected month
- **THEN** refreshed archive metadata clears that invalid selection and reloads the tab with `全部`

#### Scenario: Stale archive response arrives
- **WHEN** an older archive request resolves after authentication, tab ownership, or request generation has changed
- **THEN** the stale response cannot replace the current tab's archive state

### Requirement: Accessible and Responsive Month Archive Interaction
The month archive selector SHALL remain operable by pointer and keyboard, SHALL restore focus when dismissed, SHALL respect reduced motion, and SHALL remain within the viewport across Frux wide, compact, and narrow desktop layouts. The implementation MUST NOT depend on a third-party date-picker package or copied proprietary assets.

#### Scenario: Pointer user moves through the panel
- **WHEN** a pointer user moves from the trigger to the year or month columns
- **THEN** the panel remains open through a bounded leave grace period and supports selecting an option without pointer flicker

#### Scenario: Keyboard user operates the selector
- **WHEN** focus is on the trigger and the user opens the selector
- **THEN** Arrow, Home, End, Left, Right, Enter, Space, Tab, and Escape behavior provides logical navigation, selection, dismissal, and visible focus

#### Scenario: Selector is dismissed
- **WHEN** the user presses Escape or activates a pointer target outside the selector
- **THEN** the panel closes without changing an uncommitted navigation state and focus returns to the trigger when appropriate

#### Scenario: Narrow desktop opens the selector
- **WHEN** the profile is rendered below 1024px and the creator opens the archive panel
- **THEN** the two-column panel remains fully reachable within the viewport and does not create document-level horizontal overflow

#### Scenario: Frontend dependencies are inspected
- **WHEN** a developer reviews the Web package manifest and archive-filter assets
- **THEN** no date-picker runtime dependency, Douyin trademark, or copied proprietary SVG path has been added
