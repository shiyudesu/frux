## MODIFIED Requirements

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

## ADDED Requirements

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
