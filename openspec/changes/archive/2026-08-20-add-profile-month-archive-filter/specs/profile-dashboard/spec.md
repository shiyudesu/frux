## ADDED Requirements

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
