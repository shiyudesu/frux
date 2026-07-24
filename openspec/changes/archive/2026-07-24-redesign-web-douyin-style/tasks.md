## 1. Visual Foundation

- [x] 1.1 Create the modular `src/styles/` structure and define GCFeed shell, color, typography, spacing, radius, shadow, motion, and responsive tokens with an explicit import order.
- [x] 1.2 Implement the typed local `Icon` registry with outlined and filled states required by navigation, Feed actions, forms, messages, upload, and overlays.
- [x] 1.3 Implement original desktop and compact `BrandMark` variants with GCFeed-owned cyan/rose treatment and no copied source-site assets.
- [x] 1.4 Add shared button, input, avatar, badge, tab, status, empty-state, and overlay primitives or reusable classes that consume only the new tokens.

## 2. Application Shell

- [x] 2.1 Split the current sidebar responsibilities into route-aware `SideNav` and `MobileNav` components using the existing route, Feed-scene, session, and unread-count sources.
- [x] 2.2 Redesign `TopNav` with the 56px header geometry, presentational search control, compact utility actions, authenticated avatar state, login action, unread state, and logout behavior.
- [x] 2.3 Update `AppShell` to compose the new brand, side navigation, top navigation, mobile navigation, and content offsets with stable `data-ui` markers.
- [x] 2.4 Implement the 160px wide-desktop rail, 72px compact rail, mobile bottom navigation, active-route states, focus states, and no-horizontal-overflow behavior.
- [x] 2.5 Verify every existing user route and authentication redirect remains reachable through the redesigned shell.

## 3. Feed Stage and Playback

- [x] 3.1 Refactor `VideoStage` presentation into focused metadata, action-rail, and player-control components while keeping the existing media element, interaction callbacks, and QoS pipeline.
- [x] 3.2 Implement real play/pause, elapsed/duration, mute, seekable progress, and fullscreen state from video events, including safe behavior for image fallback items.
- [x] 3.3 Add Space playback control and update Feed keyboard handling so shortcuts do not run from inputs, textareas, selects, buttons, links, or editable content.
- [x] 3.4 Restyle the active Feed item as a rounded immersive stage with backdrop, vignette, author/follow presentation, title/description treatment, right action rail, bottom controls, and loading/end indicators.
- [x] 3.5 Preserve wheel, pointer swipe, Arrow/J/K navigation, like, favorite, follow, comment, pagination, autoplay, active-item pause, and QoS reporting across all four Feed scenes.

## 4. Details and Comments

- [x] 4.1 Replace the overlay-only `CommentPanel` composition with a `FeedDetailsPanel` that presents current-item details and the existing comment list/form using stable `data-ui` markers.
- [x] 4.2 Implement the wide-desktop two-column Feed layout so opening the 346px details panel reduces the player width and keeps the action rail attached to the player edge.
- [x] 4.3 Implement compact-desktop drawer behavior when the player minimum width would be violated.
- [x] 4.4 Implement the mobile comment bottom sheet with scroll containment, accessible close behavior, preserved input state, and no active-item change on close.
- [x] 4.5 Preserve comment loading, retry, empty, unauthenticated, submit, and public-profile navigation behavior.

## 5. Authentication and Profiles

- [x] 5.1 Redesign `LoginPage` as a dimmed short-video backdrop with a centered light authentication dialog while retaining only the supported account/password login and registration fields.
- [x] 5.2 Redesign own and public profile headers with banner treatment, circular avatars, inline counts, compact actions, and route-appropriate controls.
- [x] 5.3 Redesign profile tabs and `VideoGrid` as a dense responsive portrait work grid while preserving status labels and selection behavior.
- [x] 5.4 Restyle profile editing, relation tabs/lists, follow actions, pagination, errors, and the relation modal with the shared overlay system.
- [x] 5.5 Restyle `WorkViewer` with the shared dark media overlay and preserve media type detection, counts, playback, backdrop close, and explicit close behavior.

## 6. Messages and Upload

- [x] 6.1 Redesign `MessagesPage` as a compact dark activity list with clear unread indicators, actor information, timestamps, refresh, mark-one-read, mark-all-read, loading, empty, and error states.
- [x] 6.2 Redesign `UploadPage` as a creator workspace with a form column, file selectors, validation/status feedback, and responsive media preview while preserving the existing upload/create flow.
- [x] 6.3 Align upload, message, profile, relation, and modal controls with the shared icon, button, input, spacing, and focus conventions.

## 7. Cleanup and Documentation

- [x] 7.1 Replace every Material Symbols usage with the typed local icon system and remove the Material Symbols font import after the final usage is migrated.
- [x] 7.2 Remove obsolete legacy selectors and duplicated token values only after their replacement components pass route-level verification.
- [x] 7.3 Update `docs/uiux.md` with the new shell dimensions, token palette, Feed/details layout, player controls, profile/work recipes, authentication presentation, and responsive rules.
- [x] 7.4 Update any frontend engineering or browser-smoke documentation whose file layout or verification instructions changed.

## 8. Verification

- [x] 8.1 Run the existing frontend production build and resolve all strict TypeScript and Vite build failures.
- [x] 8.2 Run browser smoke workflows at 1440px for authentication, all four Feed scenes, comments closed/open, messages, own profile, public profile, profile editing, relations, upload, and work viewing.
- [x] 8.3 Run browser smoke workflows at 390px and a compact-desktop width to verify navigation changes, 9:16 Feed, bottom-sheet comments, touch targets, and absence of horizontal overflow.
- [x] 8.4 Capture and review desktop/mobile screenshots for the shell, Feed closed/open, authentication, profile, messages, and upload; document and resolve unexplained layout or token drift.
- [x] 8.5 Confirm required workflows produce no unexplained console errors or failed required network requests.
- [x] 8.6 Run strict OpenSpec validation and confirm the change is apply-ready.

### Verification Evidence

Desktop, compact-desktop, and mobile screenshots plus browser geometry and diagnostic reports are stored in:
`/home/shiyu/.copilot/session-state/01c64ed6-2d35-491d-89c0-384b7aeea8ee/files/gcfeed-redesign/`.
