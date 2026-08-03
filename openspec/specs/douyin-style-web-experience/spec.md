# douyin-style-web-experience Specification

## Purpose

Defines the redesigned GCFeed web experience, including the shared application shell, immersive Feed, responsive layouts, accessible interactions, and preservation of existing frontend contracts.

## Requirements

### Requirement: Unified user application shell
The user frontend SHALL render a consistent dark GCFeed shell across Feed, messages, own profile, public profile, and upload routes. At wide desktop widths the shell SHALL use a 160px navigation rail and a 56px top header, and SHALL retain the existing typed route destinations and authentication redirects.

#### Scenario: Desktop shell renders
- **WHEN** a user opens any user route at a viewport width of at least 1280px
- **THEN** the page renders the GCFeed brand, fixed 160px side navigation, 56px header, route-aware active navigation, and main content without horizontal overflow

#### Scenario: Existing destinations remain valid
- **WHEN** a user activates Feed, message, upload, profile, login, or public-profile navigation
- **THEN** the application navigates through the existing typed route union and preserves the existing authentication rules

### Requirement: Original GCFeed brand and icon system
The redesigned frontend SHALL use an original GCFeed wordmark, compact brand mark, and locally owned typed SVG icon registry. It MUST NOT embed Douyin logos, trademarks, proprietary SVG paths, or source-site artwork.

#### Scenario: Brand assets are GCFeed-owned
- **WHEN** a developer inspects the redesigned navigation and authentication surfaces
- **THEN** all brand and interface icons come from GCFeed source files or existing user/video content rather than copied Douyin assets

#### Scenario: Icon names are type checked
- **WHEN** a component requests an icon
- **THEN** the icon name is constrained by a TypeScript union and an invalid icon name fails the frontend build

### Requirement: Immersive desktop Feed stage
Each Feed scene SHALL render one active video stage inside the application shell with a rounded dark player surface, media backdrop, author metadata, vertical action rail, bottom player controls, and truthful loading, buffering, or error state. Timeline, recommendation, following, and hot scenes SHALL retain their existing data and interaction behavior, and adjacent Feed transitions SHALL reuse bounded prepared player slots when available.

#### Scenario: Feed scene uses the redesigned stage
- **WHEN** a Feed scene has an active item on wide desktop
- **THEN** the item renders inside the immersive stage with author, follow state, title, description, like, comment, favorite, share, player controls, and current playback state visible in the expected stage regions

#### Scenario: Feed scenes preserve behavior
- **WHEN** the user switches among timeline, recommendation, following, and hot routes
- **THEN** each route loads its existing scene data and supports existing swipe, wheel, keyboard, pagination, interaction, behavior-event, and QoS behavior

#### Scenario: Prepared adjacent item becomes active
- **WHEN** the user moves to an adjacent item that has a retained prepared player
- **THEN** the Feed reassigns the prepared player slot without displaying the previous item's media or rebuilding unrelated player state

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
The login/register route SHALL use a dimmed short-video backdrop and a centered light authentication dialog while preserving GCFeed's account, password, nickname, login, and registration behavior. The interface MUST NOT present nonfunctional QR, phone, or third-party authentication methods.

#### Scenario: User opens authentication
- **WHEN** an unauthenticated user navigates to the authentication route
- **THEN** the centered dialog shows the existing login/register modes and fields against the redesigned backdrop

#### Scenario: Authentication methods remain truthful
- **WHEN** the authentication dialog is rendered
- **THEN** every displayed method is supported by the existing GCFeed API and no fake QR or phone login control is shown

### Requirement: Profile and work presentation
Own and public profile routes SHALL use a full-width banner-style header, circular avatar, inline relation/work/received-like counts, compact actions, route-appropriate primary and secondary tabs, profile filters, and a dense portrait work grid. Own-profile tabs SHALL be backed by real profile-dashboard, personal-video-library, and creator-content-management APIs. Existing profile editing, relation lists, follow actions, public navigation, and work viewing SHALL remain functional.

#### Scenario: Public profile renders
- **WHEN** a public user profile loads successfully
- **THEN** the redesigned header displays the available avatar, nickname, bio, public counts, actions, public tabs, public collections, and work grid without exposing private controls or content

#### Scenario: Own profile remains editable
- **WHEN** the authenticated user opens and saves profile editing
- **THEN** profile and avatar upload requests succeed and the full-width profile header reflects the saved nickname, avatar, bio, and gender

#### Scenario: Own profile content tabs render
- **WHEN** the authenticated user opens their profile
- **THEN** Works, Recommend, Likes, Favorites, Watch History, and Watch Later tabs render with real data while Short Drama and Appointments are absent

#### Scenario: Work viewer opens
- **WHEN** a user selects a readable work card
- **THEN** the work viewer opens with the correct media, title, counts, and close behavior

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
The redesigned frontend SHALL provide explicit wide-desktop, compact-desktop/tablet, mobile, and small-mobile layouts. It SHALL avoid horizontal page overflow, preserve touch targets of at least 44px for primary mobile controls, and convert desktop threaded comments to a scrollable bottom sheet on mobile without losing sort, draft, reply-expansion, or focused-thread state.

#### Scenario: Compact desktop collapses navigation
- **WHEN** the viewport is between 901px and 1279px
- **THEN** the navigation uses its compact icon presentation and the details panel changes to drawer behavior before the player becomes unusably narrow

#### Scenario: Mobile Feed renders
- **WHEN** the viewport is 900px wide or narrower
- **THEN** the desktop rail is replaced by bottom navigation, the Feed uses a 9:16 presentation, and the page has no horizontal overflow

#### Scenario: Mobile threaded comments render as a sheet
- **WHEN** comments are opened on mobile
- **THEN** sorting, root pagination, reply expansion, comment actions, and the composer remain vertically reachable in a bottom sheet with a visible close affordance

#### Scenario: Mobile sheet preserves discussion state
- **WHEN** the user temporarily closes and reopens comments for the same active video
- **THEN** the per-video draft, selected sort, loaded roots, and expanded reply threads remain available for the current session

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
