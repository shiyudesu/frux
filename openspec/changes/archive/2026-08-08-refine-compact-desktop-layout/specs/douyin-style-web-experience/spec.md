## ADDED Requirements

### Requirement: Prioritized compact desktop controls
The shared user shell and Feed SHALL progressively compact optional labels and spacing before removing capabilities. Search, notifications, upload, identity or login, play or pause, progress, mute, fullscreen, quality, playback rate, continuous play, and Feed interaction actions SHALL remain reachable throughout the supported desktop viewport range.

#### Scenario: Compact header preserves primary actions
- **WHEN** the viewport is between 1024px and 1279px wide
- **THEN** search, notifications, upload, and identity or login remain directly reachable without horizontal document overflow

#### Scenario: Narrow header consolidates lower-priority actions
- **WHEN** the viewport is narrower than 1024px and the complete labeled header no longer fits
- **THEN** optional labels collapse and lower-priority actions move into an accessible overflow control while search and primary actions remain operable

#### Scenario: Narrow player preserves capabilities
- **WHEN** the Feed player is rendered in a narrow desktop viewport
- **THEN** optional text presentations may compact, but all supported playback capabilities remain keyboard and pointer operable through visible controls or their accessible menus

#### Scenario: Visual density does not shrink hit targets
- **WHEN** action-rail icons, metadata, or player-control glyphs use narrow density
- **THEN** their interactive controls retain usable desktop hit boxes, accessible names, and visible keyboard focus

### Requirement: Narrow Feed overlay separation
The Feed SHALL coordinate metadata, action-rail, status, and player-control insets so that narrow desktop density does not place readable content beneath another interactive region.

#### Scenario: Metadata avoids the action rail
- **WHEN** the Feed stage narrows while the action rail is visible
- **THEN** author, title, description, and status content remain inside a bounded content region that does not flow underneath the action rail

#### Scenario: Player menus remain inside the viewport
- **WHEN** a user opens quality or playback-rate controls in a compact or narrow desktop viewport
- **THEN** the menu remains visible within the stage or viewport and can be dismissed with Escape or pointer interaction

## MODIFIED Requirements

### Requirement: Functional global search control
The shared application shell SHALL render the existing top search field as a functional form that submits typed navigation to public video and user search. The control SHALL support keyboard submission, an accessible name, responsive desktop density, browser history synchronization, and truthful empty input behavior.

#### Scenario: Desktop user submits search
- **WHEN** a desktop user enters a valid term and activates the visible Search action
- **THEN** the application opens the typed search route with the encoded term

#### Scenario: Keyboard user submits search
- **WHEN** focus is in the top search input and the user presses Enter
- **THEN** the same search navigation occurs without triggering Feed keyboard shortcuts

#### Scenario: Narrow desktop renders search
- **WHEN** a user opens the shell or search route in a narrow desktop viewport
- **THEN** the search field, query, video or user tabs, results, and pagination controls remain reachable without horizontal document overflow

### Requirement: Responsive user experience
The redesigned frontend SHALL target desktop browsers with explicit wide, compact, and narrow desktop layouts. Wide desktop begins at 1280px, compact desktop covers 1024px through 1279px, and narrower viewports use the narrow desktop density. Narrow windows SHALL retain the compact desktop navigation, top header, desktop Feed presentation, and right-side drawer behavior rather than switching to a separate mobile navigation, 9:16 page shell, or bottom comment sheet. The application shell and complete Feed stage MUST NOT be adapted by applying global CSS `transform: scale()` or `zoom`.

#### Scenario: Wide desktop uses the complete shell
- **WHEN** the viewport is at least 1280px wide
- **THEN** the shell uses the 160px labeled navigation, complete top-bar presentation, and push-style desktop details panel

#### Scenario: Compact desktop collapses navigation
- **WHEN** the viewport is between 1024px and 1279px wide
- **THEN** the navigation uses its 72px icon presentation, shell spacing compacts, search width is clamped, and the details panel uses drawer behavior before the player becomes unusably narrow

#### Scenario: Narrow desktop keeps the desktop shell
- **WHEN** the viewport is narrower than 1024px
- **THEN** the 72px desktop rail, top navigation, desktop Feed composition, and desktop controls remain in use and no mobile bottom navigation is rendered

#### Scenario: Narrow density is bounded
- **WHEN** the viewport continues to narrow after minimum Feed density is reached
- **THEN** optional labels consolidate or hide while essential text, controls, focus indicators, and pointer targets do not continue shrinking without a lower bound

#### Scenario: Narrow desktop comments use a drawer
- **WHEN** comments are opened below 1280px
- **THEN** the details panel enters from the right as a dismissible modal drawer and never remains visible below the player while closed

#### Scenario: Compact drawer preserves discussion state
- **WHEN** the user temporarily closes and reopens the compact drawer for the same active video
- **THEN** the per-video draft, selected sort, loaded roots, and expanded reply threads remain available for the current session

#### Scenario: Shell geometry remains untransformed
- **WHEN** compact or narrow desktop density is active
- **THEN** fixed headers, drawers, media fullscreen, pointer gestures, and portal menus continue to use native viewport coordinates without a transformed application-shell ancestor
