## MODIFIED Requirements

### Requirement: Responsive user experience
The redesigned frontend SHALL target desktop browsers with explicit wide, compact, and narrow desktop layouts. Wide desktop begins at 1280px, compact desktop covers 1024px through 1279px, and narrower viewports use the narrow desktop density. Narrow windows SHALL retain the compact desktop navigation, top header, desktop Feed presentation, and right-side drawer behavior rather than switching to a separate mobile navigation, 9:16 page shell, or bottom comment sheet. The application shell and complete Feed stage MUST NOT be adapted by applying global CSS `transform: scale()` or `zoom`. The authenticated Following scene SHALL add a collapsible 208px people directory without changing the other Feed scenes.

#### Scenario: Wide desktop uses the complete shell
- **WHEN** the viewport is at least 1280px wide
- **THEN** the shell uses the 160px labeled navigation, complete top-bar presentation, and push-style desktop details panel

#### Scenario: Compact desktop collapses navigation
- **WHEN** the viewport is between 1024px and 1279px wide
- **THEN** the navigation uses its 72px icon presentation, shell spacing compacts, search width is clamped, and the details panel uses drawer behavior before the player becomes unusably narrow

#### Scenario: Narrow desktop keeps the desktop shell
- **WHEN** the viewport is narrower than 1024px
- **THEN** the 72px desktop rail, top navigation, desktop Feed composition, and desktop controls remain in use and no mobile bottom navigation is rendered

#### Scenario: Following directory retains desktop composition
- **WHEN** the Following scene is rendered at wide, compact, or narrow desktop width
- **THEN** its 208px directory and video stage remain sibling desktop regions until the user collapses the directory

#### Scenario: Narrow density is bounded
- **WHEN** the viewport continues to narrow after minimum Feed density is reached
- **THEN** optional labels consolidate or hide while essential text, controls, focus indicators, and pointer targets do not continue shrinking without a lower bound

#### Scenario: Narrow desktop comments use a drawer
- **WHEN** comments are opened below 1280px
- **THEN** the details panel enters from the right as a dismissible modal drawer and never remains visible below the player while closed

#### Scenario: Following push comments preserve stage width
- **WHEN** Following comments are opened between 1280px and 1439px while the directory is visible
- **THEN** the directory is temporarily removed from layout or an equivalent width-safe presentation prevents over-compressing the stage

#### Scenario: Compact drawer preserves discussion state
- **WHEN** the user temporarily closes and reopens the compact drawer for the same active video
- **THEN** the per-video draft, selected sort, loaded roots, and expanded reply threads remain available for the current session

#### Scenario: Shell geometry remains untransformed
- **WHEN** compact or narrow desktop density is active
- **THEN** fixed headers, drawers, media fullscreen, pointer gestures, directory scrolling, and portal menus continue to use native viewport coordinates without a transformed application-shell ancestor
