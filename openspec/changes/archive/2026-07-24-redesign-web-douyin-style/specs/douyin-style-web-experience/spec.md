## ADDED Requirements

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
Each Feed scene SHALL render one active video stage inside the application shell with a rounded dark player surface, media backdrop, author metadata, vertical action rail, and bottom player controls. Timeline, recommendation, following, and hot scenes SHALL retain their existing data and interaction behavior.

#### Scenario: Feed scene uses the redesigned stage
- **WHEN** a Feed scene has an active item on wide desktop
- **THEN** the item renders inside the immersive stage with author, follow state, title, description, like, comment, favorite, share, and player controls visible in the expected stage regions

#### Scenario: Feed scenes preserve behavior
- **WHEN** the user switches among timeline, recommendation, following, and hot routes
- **THEN** each route loads its existing scene data and supports existing swipe, wheel, keyboard, pagination, interaction, and QoS behavior

### Requirement: Real media controls
For video media, the Feed stage SHALL expose play/pause, elapsed and duration values, mute state, seekable progress, and fullscreen controls backed by the active media element. Image fallback stages SHALL NOT display false playback progress or enabled video-only controls.

#### Scenario: User pauses and resumes playback
- **WHEN** the active video is playing and the user activates the play control or presses Space outside an editable field
- **THEN** the media pauses, the control state updates, and the same action resumes playback

#### Scenario: User seeks the video
- **WHEN** the user selects a valid position on the progress control
- **THEN** the active video's current time moves to the corresponding position and the elapsed display updates

#### Scenario: Non-video item renders safely
- **WHEN** the Feed item resolves to an image rather than a playable video
- **THEN** the image remains visible and video-only controls are hidden or disabled

### Requirement: Desktop details and comments panel
On wide desktop, opening comments SHALL add a 346px details panel beside the active player and SHALL reduce the player column rather than covering it. The panel SHALL expose available item details and the existing comment list and form without adding unsupported AI or private source-site features.

#### Scenario: Comment panel pushes the stage
- **WHEN** the user opens comments at a viewport width of at least 1280px
- **THEN** a 346px side panel becomes visible, the player width decreases, and the action rail remains attached to the player edge

#### Scenario: Comment behavior is preserved
- **WHEN** comments are opened for the current item
- **THEN** existing loading, error, empty, list, retry, authenticated submit, and unauthenticated-disabled states remain available

#### Scenario: Closing comments restores the stage
- **WHEN** the user closes the details panel
- **THEN** the Feed returns to a single player column without changing the active item

### Requirement: Douyin-style authentication presentation
The login/register route SHALL use a dimmed short-video backdrop and a centered light authentication dialog while preserving GCFeed's account, password, nickname, login, and registration behavior. The interface MUST NOT present nonfunctional QR, phone, or third-party authentication methods.

#### Scenario: User opens authentication
- **WHEN** an unauthenticated user navigates to the authentication route
- **THEN** the centered dialog shows the existing login/register modes and fields against the redesigned backdrop

#### Scenario: Authentication methods remain truthful
- **WHEN** the authentication dialog is rendered
- **THEN** every displayed method is supported by the existing GCFeed API and no fake QR or phone login control is shown

### Requirement: Profile and work presentation
Own and public profile routes SHALL use a banner-style header, circular avatar, inline relation/work counts, compact actions, route-appropriate tabs, and a dense portrait work grid. Existing profile editing, relation lists, follow actions, public navigation, and work viewing SHALL remain functional.

#### Scenario: Public profile renders
- **WHEN** a public user profile loads successfully
- **THEN** the redesigned header displays the available avatar, nickname, bio, counts, actions, and work grid without exposing private controls

#### Scenario: Own profile remains editable
- **WHEN** the authenticated user opens and saves profile editing
- **THEN** the existing profile update and avatar upload requests succeed and the redesigned profile reflects the saved values

#### Scenario: Work viewer opens
- **WHEN** a user selects a work card
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
The redesigned frontend SHALL provide explicit wide-desktop, compact-desktop/tablet, mobile, and small-mobile layouts. It SHALL avoid horizontal page overflow, preserve touch targets of at least 44px for primary mobile controls, and convert desktop comments to a bottom sheet on mobile.

#### Scenario: Compact desktop collapses navigation
- **WHEN** the viewport is between 901px and 1279px
- **THEN** the navigation uses its compact icon presentation and the details panel changes to drawer behavior before the player becomes unusably narrow

#### Scenario: Mobile Feed renders
- **WHEN** the viewport is 900px wide or narrower
- **THEN** the desktop rail is replaced by bottom navigation, the Feed uses a 9:16 presentation, and the page has no horizontal overflow

#### Scenario: Mobile comments render as a sheet
- **WHEN** comments are opened on mobile
- **THEN** they appear as a vertically scrollable bottom sheet that leaves a visible close affordance and preserves the comment form state

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
The redesign SHALL NOT add or change backend endpoints, response shapes, local-storage formats, route-library dependencies, or the TypeScript strictness requirements.

#### Scenario: Production build succeeds
- **WHEN** the redesigned frontend is built with the existing build script
- **THEN** `tsc --noEmit` and `vite build` complete without type suppressions or explicit `any`

#### Scenario: API boundaries are unchanged
- **WHEN** a developer compares the redesigned page workflows with the existing API modules
- **THEN** the same typed API functions and response interfaces are used for authentication, Feed, interactions, messages, profiles, relations, and upload
