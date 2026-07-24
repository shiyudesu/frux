## Context

The current React/Vite frontend already has the required product flows and a useful short-video foundation: typed routes, session contexts, four Feed scenes, one-item swipe navigation, a video stage, action rail, comments, profiles, messages, upload, relations, and work viewing. Presentation is concentrated in a large global stylesheet and currently uses a 220px sidebar, 64px top bar, broad glass-card surfaces, Material Symbols, and a comment panel that overlays the Feed.

Public Douyin pages were inspected in an isolated Windows Chrome profile. The verified desktop references use a 160px fixed navigation rail, a 56px header, a `#161823`-class dark background, a rounded immersive video stage, a compact right action rail, bottom player controls, a 346px details/comment panel that reduces the player width, a profile banner with a circular avatar and dense portrait work grid, and a centered light authentication dialog. Authenticated message and creator-management surfaces were not inspected and must be designed by applying the verified public design language to GCFeed's existing workflows.

The implementation must preserve strict TypeScript, the hand-written router, existing API modules and response types, session distribution through hooks, all loading/error/empty states, and existing backend contracts. GCFeed must not copy Douyin trademarks, logos, proprietary SVG paths, video assets, or protected copy.

## Goals / Non-Goals

**Goals:**

- Establish one coherent GCFeed visual system for every user-facing route.
- Match the verified desktop proportions and interaction density while retaining GCFeed identity and functionality.
- Make Feed comments part of the desktop layout rather than an overlay over the video.
- Replace network-loaded Material Symbols with a typed local icon registry.
- Add real player-state presentation for play/pause, mute, progress, seek, and fullscreen while preserving QoS reporting.
- Keep tablet and mobile layouts usable without reproducing the observed desktop site's horizontal overflow.
- Make the redesign verifiable through stable DOM markers, browser geometry assertions, screenshots, and the existing production build.

**Non-Goals:**

- Copying the Douyin logo, wordmark, QR login, proprietary artwork, or exact private-account pages.
- Adding phone/QR/password login methods that the GCFeed backend does not support.
- Implementing search results, AI search, live streaming, short dramas, games, or creator analytics.
- Changing Feed ranking, API schemas, authentication rules, upload semantics, message semantics, or persistence.
- Replacing the typed hand-written router or introducing a new component framework.

## Decisions

### 1. Build an original token system from verified proportions

Create a GCFeed-owned token layer with semantic variables rather than retaining source-site names:

- Shell: `--gc-sidebar-width: 160px`, `--gc-header-height: 56px`, `--gc-detail-width: 346px`.
- Core surfaces: near-black `#161823`, raised dark surfaces around `#1c1e29` and `#252733`, white primary text, and muted white text.
- Accents: GCFeed-owned cyan and rose values inspired by short-video conventions, used as a paired brand treatment rather than a copied logo.
- Geometry: 16px primary stage radius, compact 8-12px controls, and pill actions only where the interaction is pill-shaped.

The existing `styles.css` will become an ordered entry point for modular files under `src/styles/` covering tokens/base, shell, Feed, pages, overlays, and responsive rules. This is preferred over continuing to grow one file because the redesign crosses every route and needs clear ownership. CSS Modules or a new styling dependency would add migration cost without improving runtime behavior.

### 2. Preserve routing and data flow while replacing shell composition

`AppShell` remains the authenticated/public user layout boundary, but its presentation is decomposed into:

- `BrandMark`: an original GCFeed wordmark and compact mark.
- `SideNav`: route-aware grouped navigation for Feed scenes, messages, upload, and profile.
- `TopNav`: search presentation, compact utility actions, login/avatar, unread state, and logout.
- `MobileNav`: bottom navigation rendered only at the compact breakpoint.
- `Icon`: a typed inline-SVG registry with an `IconName` union.

The existing `FEED_SCENES`, `useRoute`, `useNavigate`, `useSession`, and `useUnreadCount` remain the state sources. Navigation labels may be shortened for density, but route meanings and authentication redirects remain unchanged.

The search control remains presentational until a search capability exists. It must not imply successful search results or add an untyped route.

### 3. Keep Feed orchestration in `FeedPage` and split presentation inside the stage

`FeedPage` retains data loading, interaction mutations, scene selection, swipe state, comment state, QoS reporting, and keyboard navigation. Presentation is divided into focused components:

- `VideoStage`: active media, backdrop, player state, and composition root.
- `FeedMetadata`: author, follow state, title, description, and tags.
- `FeedActionRail`: avatar/follow affordance plus like, comment, favorite, share, and more actions.
- `FeedPlayerControls`: play/pause, elapsed/duration, mute, progress/seek, and fullscreen.
- `FeedDetailsPanel`: current-item details and comments using existing Feed and comment data.

This avoids rewriting application behavior while making the dense player UI maintainable. `VideoStage` remains responsible for the single video element so playback controls and QoS events do not compete over multiple refs.

### 4. Make player controls reflect real media state

The current static progress width is replaced with state derived from video events (`loadedmetadata`, `timeupdate`, `play`, `pause`, `volumechange`, and `ended`). Space toggles playback when focus is not inside an editable control. Progress interaction seeks the active video, mute state controls the actual element, and fullscreen uses the browser Fullscreen API with an explicit unsupported/error state.

Existing autoplay, active-stage pausing, loop behavior, first-frame metrics, stutter counting, and QoS flushing remain intact. Controls for non-video fallback images are hidden or disabled instead of presenting false playback state.

### 5. Use a responsive two-column Feed layout for desktop comments

At wide desktop sizes the Feed content area uses:

```text
┌─────────────── 160px navigation ───────────────┐
│                                                │
│  56px header                                   │
│  ┌──────────── player ────────────┬─ 346px ─┐  │
│  │                                │ details │  │
│  │                                │ comments│  │
│  └────────────────────────────────┴─────────┘  │
└────────────────────────────────────────────────┘
```

Opening comments changes the content grid from one column to `minmax(0, 1fr) 346px`; it does not cover the video. The action rail remains attached to the player edge and moves with the reduced player width. The panel provides existing item details and comments; unsupported source-site tabs such as AI features are not rendered.

When the available player width would fall below the product's usable minimum, the detail panel becomes an overlay drawer. At mobile widths it becomes a bottom sheet capped below the top header. This is preferred over forcing the desktop geometry onto small screens.

### 6. Apply one page language without forcing every page into Feed geometry

- **Authentication:** a dimmed short-video preview backdrop with a centered light dialog. GCFeed account/password and login/register modes remain the only functional methods; no fake QR or phone controls are shown.
- **Own/public profiles:** banner-style hero, 112px circular avatar on desktop, inline relation/work counts, compact actions, tabs, and a dense portrait work grid. Existing edit, relation, follow, and viewer behavior remains.
- **Messages:** flat dark list rows, compact category/unread presentation, rose unread indicators, and existing refresh/read actions.
- **Upload:** dark creator-workspace surface with a form column and portrait/landscape preview column, preserving current video and cover uploads.
- **Relations and work viewer:** dark focused overlays using the same tab, button, avatar, spacing, and motion tokens.

These pages reuse shared primitives and tokens but retain layouts appropriate to their workflow.

### 7. Use original inline SVG icons and remove the icon-font dependency

A local typed icon registry replaces text ligatures from Material Symbols. Icons are authored for GCFeed and exposed through a consistent size/stroke/fill API. Active Feed states may use filled variants, while navigation and utility actions use outlined variants.

This removes a network font dependency, avoids layout shifts while the icon font loads, provides compile-time icon-name checking, and prevents accidental reuse of proprietary source SVGs. Adding a third-party icon package was considered but rejected because the required set is small and brand-specific.

### 8. Define explicit responsive tiers

- **Wide desktop (`>= 1280px`):** 160px rail, 56px header, pushing 346px detail panel, dense profile/work grids.
- **Compact desktop/tablet (`901-1279px`):** 72px icon rail, compact header actions, reduced page padding, and detail drawer behavior when the stage cannot retain its minimum width.
- **Mobile (`<= 900px`):** no desktop rail, fixed bottom navigation, 9:16 Feed stage, bottom-sheet comments, single-column forms, and reduced profile grids.
- **Small mobile (`<= 560px`):** icon-first controls, tighter metadata, hidden secondary description text, and touch targets of at least 44px.

All tiers must avoid horizontal page overflow and preserve keyboard focus visibility. Motion honors `prefers-reduced-motion`.

### 9. Expose stable verification markers

Key structures receive stable `data-ui` attributes such as `app-shell`, `side-nav`, `top-nav`, `feed-stage`, `action-rail`, `player-controls`, `details-panel`, `mobile-nav`, `profile-hero`, and `work-grid`. Browser smoke verification uses these markers and bounding rectangles rather than obfuscated styling classes or copy-sensitive selectors.

Visual verification covers 1440px desktop and representative compact/mobile widths. Screenshots are reference evidence during implementation, while structural assertions remain the durable automated contract.

### 10. Migrate incrementally and keep business logic stable

Implementation order is tokens/icons, shell, Feed, overlays, remaining pages, responsive behavior, documentation, and verification. Existing API and hook logic is moved only when a presentational boundary requires it. Old selectors are removed after their replacement route passes build and browser verification.

## Risks / Trade-offs

- **[Risk] Visual similarity could drift into copied branding or assets.** → Use an original GCFeed mark, locally authored icons, existing GCFeed/user media, and semantic tokens; never download or commit Douyin trademarks or proprietary SVG paths.
- **[Risk] The comment panel can make the player too narrow on medium screens.** → Enforce a player minimum width and switch the panel to a drawer before that threshold is crossed.
- **[Risk] Playback controls can regress QoS collection or autoplay.** → Keep one video ref and one event pipeline; add controls around the existing QoS handlers rather than replacing them.
- **[Risk] Splitting global CSS can introduce ordering regressions.** → Use one explicit style entry point with a documented import order and migrate route groups incrementally.
- **[Risk] Authenticated Douyin surfaces were not observed.** → Apply verified public tokens and component patterns to GCFeed's own message/upload workflows and document that these are product adaptations, not claimed copies.
- **[Risk] Blur, large images, and overlays can hurt low-end devices.** → Limit blur layers, disable decorative effects at compact breakpoints, and honor reduced motion.
- **[Trade-off] The redesign adds real player controls beyond a pure restyle.** → This is accepted because the current static progress indicator is misleading and the existing UI/UX document already requires play/pause behavior.

## Migration Plan

1. Add the new token/style structure, original icon registry, and shared primitives while leaving existing page logic intact.
2. Replace `AppShell`, `TopNav`, and sidebar presentation; verify every route remains reachable.
3. Refactor the Feed presentation and introduce real playback controls, then implement the responsive details/comment panel.
4. Migrate authentication, profiles, work cards/viewer, relations, messages, and upload route by route.
5. Remove obsolete Material Symbols imports and legacy selectors after all usages are migrated.
6. Update `docs/uiux.md`, run the production build, execute browser smoke workflows at desktop and mobile widths, and review screenshots against the captured public references.

Rollback is a frontend-only revert to the previous assets/components/styles. No data migration or backend rollback is required.

## Open Questions

No blocking product decisions remain. Search behavior, QR/phone login, AI features, live streaming, and creator analytics remain separate future capabilities rather than placeholders in this redesign.
