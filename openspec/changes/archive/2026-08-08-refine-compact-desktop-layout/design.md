## Context

Frux is intentionally a desktop short-video application. The existing responsive layer collapses the side navigation from 160px to 72px below 1280px and turns comments into a right-side drawer, while the former mobile navigation and bottom-sheet rules are disabled. The remaining shell and Feed UI continue to use wide-desktop spacing and visibility, so the top bar and player overlays become crowded as the viewport narrows.

Chrome DevTools measurements of the current Douyin desktop Feed showed that it does not apply a global `transform: scale()` or `zoom` to the application or player ancestors. Instead, it preserves the desktop composition while progressively changing density:

- At 1280px the side navigation is 160px and the complete header is visible.
- Around 1200px the side navigation becomes a 72px icon rail.
- At 1024px lower-priority header actions are consolidated and login becomes compact.
- At 800px the icon rail, top header, full-height Feed, action column, and desktop controls remain; no mobile shell is introduced.

Frux must preserve its strict TypeScript frontend, hand-written router, existing Feed gestures and player adapters, accessible controls, and dependency-free CSS architecture.

## Goals / Non-Goals

**Goals:**

- Preserve a coherent desktop composition from wide through narrow browser windows.
- Define predictable wide, compact, and narrow desktop density behavior.
- Keep search, upload, notifications, identity, playback, Feed interaction, and comments reachable without horizontal clipping.
- Compact visual metrics and optional labels without shrinking the entire application or reducing essential pointer and keyboard targets below usable desktop sizes.
- Keep comments as a right-side overlay drawer below the wide breakpoint.
- Add repeatable geometry and screenshot verification at representative viewport widths.

**Non-Goals:**

- Adding a phone-specific bottom navigation, 9:16 page shell, or bottom comment sheet.
- Applying `transform: scale()` or CSS `zoom` to `.app-shell`, `.app-body`, or the complete Feed stage.
- Changing backend APIs, Feed data behavior, player adapter behavior, typed routes, or authentication rules.
- Copying Douyin assets, class names, source code, or exact visual styling.
- Redesigning admin pages or unrelated user-page content layouts.

## Decisions

### 1. Use three desktop density tiers

The responsive system will distinguish:

- **Wide desktop (`>=1280px`)**: 160px labeled side navigation, full top-bar labels, full Feed presentation, and push-style details panel.
- **Compact desktop (`1024px-1279px`)**: 72px icon navigation, reduced shell gaps, clamped search width, compact header labels, and overlay details drawer.
- **Narrow desktop (`<1024px`)**: the same 72px desktop rail and header remain, lower-priority labels/actions compact further, Feed overlay metrics use their minimum density, and comments remain a right-side drawer.

Density values will be bounded. Once the minimum narrow density is reached, additional pressure will be handled by hiding optional labels, consolidating actions, and clamping content rather than continuously shrinking text and controls.

**Alternative considered:** Retain the current single breakpoint. Rejected because it changes only the side rail and leaves the primary sources of narrow-window crowding unchanged.

### 2. Keep the shell in native layout coordinates

The shell and Feed layout will continue to use normal CSS layout. Shared custom properties will express bounded visual density, such as compact gaps, action insets, overlay font sizes, and icon sizes. Media, fixed headers, drawers, portals, and interaction hit boxes will not be placed inside a globally transformed wrapper.

This avoids transformed containing blocks, mismatched pointer coordinates, blurry text, incorrect fullscreen geometry, portal alignment problems, and unexpectedly small focus/click targets.

**Alternative considered:** Scale a fixed 1280px logical canvas to the viewport. Rejected because the Feed depends on pointer gestures, native media fullscreen, fixed drawers, menus, and accessible controls whose geometry should remain in CSS pixels.

### 3. Prioritize top-bar actions instead of allowing overflow

The top bar will preserve this priority order:

1. Search remains visible and keyboard-submittable.
2. Identity/login and notifications remain directly reachable.
3. Upload remains directly reachable but may become icon-only.
4. Logout and other lower-priority actions may move into an accessible overflow control in narrow mode.

The search field will use a clamped width instead of a fixed `minmax(280px, 560px)` floor. Compact and narrow modes will reduce header padding and gaps. Guest login will collapse from icon plus text to an icon-sized control with the same accessible name.

The overflow control, when rendered, will use an explicit button, menu semantics, keyboard operation, outside-click/Escape dismissal, and deterministic focus return.

**Alternative considered:** Hide the search field below a breakpoint. Rejected because search is a primary global destination and already has a typed, functional route contract.

### 4. Compact Feed overlays while preserving capabilities

The stage will continue to consume the available desktop content area. CSS variables will coordinate:

- The right interaction inset used by metadata and status content.
- Action-rail visual icon size and vertical gap.
- Metadata offsets, maximum width, title size, and description line count.
- Player-control gaps, selected-value labels, and optional text visibility.

The action rail's visible glyphs may shrink, but its buttons will retain usable hit boxes. Metadata will never flow underneath the action rail. Player controls will compact in priority order:

1. Progress, play/pause, mute, and fullscreen remain directly available.
2. Effective quality and playback-rate controls remain operable through their existing menus.
3. Continuous-play text may hide while its switch and accessible name remain.
4. Time and selected-value labels may use shorter presentations before controls are removed.

No supported playback capability will disappear solely because the viewport is narrow.

**Alternative considered:** Scale the complete stage including controls. Rejected because it would reduce target sizes and couple media rendering to UI density.

### 5. Keep compact comments independent from Feed density

At widths below 1280px the details surface remains a fixed-position right drawer with a scrim and dialog semantics. Its width will be clamped to the available viewport so it does not depend on the scaled Feed overlay metrics. Opening and closing the drawer must not resize, transform, or replace the active Feed item.

### 6. Prefer CSS media queries and stable geometry markers

Responsive presentation will be driven primarily by CSS media queries and custom properties. React viewport listeners will not be introduced for visual-only changes. Existing JavaScript media-query state remains limited to semantic behavior that changes the details panel between complementary panel and modal drawer.

Stable `data-ui` markers will continue to identify the shell, header, Feed stage, action rail, player controls, and details panel for browser geometry checks.

### 7. Verify a viewport matrix rather than one narrow breakpoint

Browser verification will cover at least:

- 1440px wide desktop.
- 1200px compact desktop.
- 1024px compact-to-narrow boundary.
- 800px narrow desktop.

Checks will cover horizontal overflow, shell geometry, search reachability, visible primary actions, Feed metadata/action separation, player-control reachability, comment drawer operation, and screenshots with comments closed and open.

## Risks / Trade-offs

- **[Risk] Compact labels may make controls harder to understand** -> Preserve accessible names, tooltips/menu labels where already supported, and keep key selected values visible as space permits.
- **[Risk] Visual glyphs and hit boxes can drift apart** -> Size buttons independently from icons and verify both geometry and click behavior.
- **[Risk] Popover menus may leave the viewport near the right edge** -> Anchor menus to the available stage edge and add browser checks at 1024px and 800px.
- **[Risk] The comment drawer may obscure nearly all Feed content at very narrow widths** -> Clamp drawer width to the viewport and treat it as a modal dialog with a scrim and reliable close/focus behavior.
- **[Risk] Multiple media queries can become inconsistent** -> Centralize tier thresholds and density values in responsive tokens rather than repeating unrelated one-off values.
- **[Trade-off] The interface remains desktop-oriented at phone-sized widths** -> This is intentional; the change improves narrow desktop use without introducing a second mobile product surface.

## Migration Plan

1. Add shared density and inset tokens for the three desktop tiers.
2. Refine shell and top-bar CSS, then adjust `TopNav` only where semantic overflow behavior is required.
3. Apply the density tokens to Feed metadata, action rail, player controls, and status overlays.
4. Confirm the existing details drawer remains independent and accessible.
5. Add or update focused component tests and browser geometry/visual evidence.
6. Update `docs/uiux.md` and mark the small-screen issue resolved only after browser verification.

The change is frontend-only and can be rolled back by reverting the responsive styles and any additive top-bar overflow component without data migration.

## Open Questions

- Exact compact and narrow density values will be tuned during browser screenshot comparison, but they must remain within the behavioral constraints in the specs.
- If the existing top-bar actions fit after label compaction, the overflow menu may contain only logout; it must not be introduced as an empty or nonfunctional control.
