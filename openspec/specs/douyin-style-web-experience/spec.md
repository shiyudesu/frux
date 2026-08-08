# douyin-style-web-experience Specification

## Purpose

Defines the redesigned Frux web experience, including the shared application shell, immersive Feed, responsive layouts, accessible interactions, and preservation of existing frontend contracts.

## Requirements

### Requirement: Unified user application shell
The user frontend SHALL render a consistent dark Frux shell across Feed, messages, own profile, public profile, and upload routes. At wide desktop widths the shell SHALL use a 160px navigation rail and a 56px top header, and SHALL retain the existing typed route destinations and authentication redirects.

#### Scenario: Desktop shell renders
- **WHEN** a user opens any user route at a viewport width of at least 1280px
- **THEN** the page renders the Frux brand, fixed 160px side navigation, 56px header, route-aware active navigation, and main content without horizontal overflow

#### Scenario: Existing destinations remain valid
- **WHEN** a user activates Feed, message, upload, profile, login, or public-profile navigation
- **THEN** the application navigates through the existing typed route union and preserves the existing authentication rules

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

### Requirement: Original Frux brand and icon system
The redesigned frontend SHALL use an original FRUX wordmark, F compact brand mark, and locally owned typed SVG icon registry. It MUST NOT embed Douyin logos, trademarks, proprietary SVG paths, or source-site artwork.

#### Scenario: Brand assets are Frux-owned
- **WHEN** a developer inspects the redesigned navigation and authentication surfaces
- **THEN** all brand and interface icons come from Frux source files or existing user/video content rather than copied Douyin assets

#### Scenario: Icon names are type checked
- **WHEN** a component requests an icon
- **THEN** the icon name is constrained by a TypeScript union and an invalid icon name fails the frontend build

### Requirement: Immersive desktop Feed stage
Each Feed scene SHALL render one active video stage inside the application shell with a rounded dark player surface, media backdrop, author metadata, vertical action rail, bottom player controls, and truthful loading, buffering, or error state. Timeline, recommendation, following, and hot scenes SHALL retain their existing data and interaction behavior, adjacent Feed transitions SHALL reuse bounded prepared player slots when available, and switching among Feed routes SHALL restore each valid retained scene to its previous active video instead of unconditionally restarting from its first card.

#### Scenario: Feed scene uses the redesigned stage
- **WHEN** a Feed scene has an active item on wide desktop
- **THEN** the item renders inside the immersive stage with author, follow state, title, description, like, comment, favorite, share, player controls, and current playback state visible in the expected stage regions

#### Scenario: Feed scenes preserve behavior
- **WHEN** the user switches among timeline, recommendation, following, and hot routes
- **THEN** each route supports its existing swipe, wheel, keyboard, pagination, interaction, behavior-event, and QoS behavior while maintaining independent retained Feed data

#### Scenario: User returns to a previous Feed route
- **WHEN** the user advances within one Feed scene, visits another Feed route, and returns during the same mounted Feed session
- **THEN** the previous scene restores its retained active video, ordering, request identity, and forward-pagination state without an unnecessary first-page request

#### Scenario: Prepared adjacent item becomes active
- **WHEN** the user moves to an adjacent item that has a retained prepared player
- **THEN** the Feed reassigns the prepared player slot without displaying the previous item's media or rebuilding unrelated player state

#### Scenario: Restored scene rebuilds transient player state
- **WHEN** a previously inactive Feed scene is restored
- **THEN** its active card uses a fresh visible player lifecycle and does not restore stale comments, gestures, menus, buffering, fullscreen, or playback-time state

#### Scenario: User explicitly refreshes the active Feed destination
- **WHEN** the user activates the refresh control beside the active Feed destination in the left navigation
- **THEN** its first page replaces its retained snapshot, other Feed destinations keep their retained positions, and inactive destinations do not show refresh icons

### Requirement: Real media controls
For video media, the Feed stage SHALL expose play/pause, elapsed and duration values, mute state, seekable progress, fullscreen, quality, playback-rate, buffering, retry, and continuous-play controls backed by the active player adapter. Image fallback stages SHALL NOT display false playback progress or enabled video-only controls.

#### Scenario: User pauses and resumes playback
- **WHEN** the active video is playing and the user activates the play control or presses Space outside an editable field
- **THEN** the media pauses, the control state updates, and the same action resumes playback

#### Scenario: User seeks the video
- **WHEN** the user selects a valid position on the progress control
- **THEN** the active video's current time moves to the corresponding position and the elapsed display updates

#### Scenario: User selects quality
- **WHEN** multiple compatible qualities are available and the user selects one
- **THEN** the active player applies or attempts that quality and reflects the effective selection or fallback

#### Scenario: Video is buffering
- **WHEN** the active player lacks enough data to continue expected playback
- **THEN** the stage displays a truthful buffering state until playback resumes or an error is surfaced

#### Scenario: Non-video item renders safely
- **WHEN** the Feed item resolves to an image rather than a playable video
- **THEN** the image remains visible and video-only controls are hidden or disabled

### Requirement: Desktop details and comments panel
On wide desktop, opening comments SHALL add a 346px details panel beside the active player and SHALL reduce the player column rather than covering it. The panel SHALL expose item details plus a complete threaded comment surface with hot/latest sorting, root pagination, bounded reply previews, reply expansion, comment likes, permission-aware deletion, reply composition, and truthful loading, empty, busy, and error states.

#### Scenario: Comment panel pushes the stage
- **WHEN** the user opens comments at a viewport width of at least 1280px
- **THEN** a 346px side panel becomes visible, the player width decreases, and the action rail remains attached to the player edge

#### Scenario: Threaded comments are operable
- **WHEN** comments are opened for the current item
- **THEN** the user can switch hot/latest sorting, load additional roots, expand and collapse replies, like visible comments, target a reply, and use allowed delete controls without changing the active Feed item

#### Scenario: Comment composer reflects authentication and target
- **WHEN** an authenticated user selects a root or reply target
- **THEN** the composer identifies the target, preserves a per-video draft, enforces the content limit, and exposes independent submitting and failure states

#### Scenario: Unauthenticated user attempts to participate
- **WHEN** an unauthenticated user activates comment, reply, or comment-like controls
- **THEN** public discussion remains readable and the interface offers a functional login action rather than only a disabled input

#### Scenario: Closing comments restores the stage
- **WHEN** the user closes the details panel
- **THEN** the Feed returns to a single player column without changing the active item and focus returns to the comment action

### Requirement: Douyin-style authentication presentation
The login/register route SHALL use a dimmed short-video backdrop and a centered light authentication dialog while preserving Frux's account, password, nickname, login, and registration behavior. The interface MUST NOT present nonfunctional QR, phone, or third-party authentication methods.

#### Scenario: User opens authentication
- **WHEN** an unauthenticated user navigates to the authentication route
- **THEN** the centered dialog shows the existing login/register modes and fields against the redesigned backdrop

#### Scenario: Authentication methods remain truthful
- **WHEN** the authentication dialog is rendered
- **THEN** every displayed method is supported by the existing Frux API and no fake QR or phone login control is shown

### Requirement: Profile and work presentation
Own and public profile routes SHALL use a full-width banner-style header, circular avatar, inline relation/work/received-like counts, compact actions, route-appropriate primary and secondary tabs, profile filters, and a dense portrait work grid. Own-profile tabs SHALL be backed by real profile-dashboard, personal-video-library, and creator-content-management APIs and SHALL NOT include a personal Recommend tab. Selecting a readable personal-library card SHALL open the immersive collection queue, while creator work and collection behavior SHALL remain truthful for their visibility and ownership context. Existing profile editing, relation lists, follow actions, public navigation, and work viewing SHALL remain functional.

#### Scenario: Public profile renders
- **WHEN** a public user profile loads successfully
- **THEN** the redesigned header displays the available avatar, nickname, bio, public counts, actions, public tabs, public collections, and work grid without exposing private controls or content

#### Scenario: Own profile remains editable
- **WHEN** the authenticated user opens and saves profile editing
- **THEN** profile and avatar upload requests succeed and the full-width profile header reflects the saved nickname, avatar, bio, and gender

#### Scenario: Own profile content tabs render
- **WHEN** the authenticated user opens their profile
- **THEN** Works, Likes, Favorites, Watch History, and Watch Later tabs render with real data while Recommend, Short Drama, and Appointments are absent

#### Scenario: Personal library queue opens
- **WHEN** a user selects a readable card from Likes, Favorites, Watch History, or Watch Later
- **THEN** an immersive full-screen player opens at the selected item and can continue through that ordered collection

#### Scenario: Creator work viewer remains truthful
- **WHEN** a user selects a creator work whose context is not a personal-library queue
- **THEN** the work opens through the supported viewer behavior with the correct media, title, visibility permissions, counts, and close behavior

### Requirement: Consistent messages, upload, and relation surfaces
Messages, upload, relation lists, profile editing, and work-viewer overlays SHALL use the shared dark token system, typography, button hierarchy, icons, spacing, and state presentation while preserving their existing API behavior.

#### Scenario: Messages remain operable
- **WHEN** an authenticated user visits messages and uses refresh or read controls
- **THEN** the redesigned message list shows loading, error, empty, read, and unread states and issues the same API requests as before

#### Scenario: Upload remains operable
- **WHEN** an authenticated user selects valid video and cover files and submits the upload form
- **THEN** the redesigned creator workspace uploads both files, creates the video, and navigates according to the existing success flow

#### Scenario: Relation overlay remains operable
- **WHEN** the user opens following or follower relations
- **THEN** the redesigned overlay supports tab switching, pagination, retry, and available follow actions

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

### Requirement: Typed video discussion destination
The frontend SHALL add a typed `/videos/{videoId}` route using the existing hand-written History API router. The route SHALL support validated comment-focus search parameters and SHALL reuse shared video and threaded-comment components without adding a routing library.

#### Scenario: Video discussion route opens directly
- **WHEN** a user navigates to a valid video-detail URL with root and target comment parameters
- **THEN** the readable video renders with its comment panel focused on the requested discussion

#### Scenario: Invalid discussion parameters are supplied
- **WHEN** comment-focus search values are missing, malformed, or inconsistent
- **THEN** the route still renders the readable video and presents a safe unfocused or unavailable-comment state

#### Scenario: Invalid typed route is authored
- **WHEN** frontend code attempts to navigate to an unsupported video-detail path
- **THEN** strict TypeScript compilation rejects the invalid navigation target

### Requirement: Accessible and reduced-motion interaction
The redesigned controls SHALL expose accessible names, visible keyboard focus, logical tab order, sufficient contrast, and reduced-motion behavior. Existing keyboard Feed shortcuts SHALL remain available and SHALL NOT fire while the user is editing an input or textarea.

#### Scenario: Keyboard user navigates controls
- **WHEN** a keyboard user tabs through navigation, Feed actions, dialogs, and forms
- **THEN** focus remains visible and each interactive control has an accessible name

#### Scenario: Editable controls suppress Feed shortcuts
- **WHEN** focus is inside an input, textarea, select, or editable element
- **THEN** Feed navigation and playback shortcuts do not intercept the user's text editing keys

#### Scenario: Reduced motion is requested
- **WHEN** the operating system reports `prefers-reduced-motion: reduce`
- **THEN** nonessential transitions and decorative motion are removed or shortened without preventing navigation or state changes

### Requirement: Existing frontend contracts remain stable
The redesigned frontend SHALL preserve existing typed routes, local-storage validation, strict TypeScript, authentication redirects, and existing API functions while using additive profile, personal-library, creator-management, and privacy APIs. Existing endpoint response shapes MUST remain compatible unless a new endpoint is used for the expanded behavior.

#### Scenario: Production build succeeds
- **WHEN** the redesigned page and new typed API modules are built with the existing build script
- **THEN** `tsc --noEmit` and `vite build` complete without type suppressions, explicit `any`, or a routing-library dependency

#### Scenario: Existing API boundaries remain compatible
- **WHEN** an existing client continues using authentication, Feed, interactions, messages, simple profile, relations, upload, and simple video-list APIs
- **THEN** those calls remain valid while new profile capabilities use additive typed endpoints and fields
