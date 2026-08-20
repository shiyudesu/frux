## Why

The creator work toolbar currently exposes two browser-native date inputs whose appearance and calendar popup do not match the Frux profile experience. A Douyin-style year/month archive selector gives creators a compact, predictable way to jump to months that actually contain their works without adding a third-party date-picker dependency.

## What Changes

- Replace the own-profile work date inputs with a Frux-owned archive trigger containing a calendar icon, the current month selection, and a disclosure indicator.
- Present a dark two-column archive panel with `全部` and descending years on the left and available months for the active year on the right.
- Apply a selected month immediately while preserving independent public/private work filters, pagination reset, keyword filtering, and existing creator-query ordering.
- Add an authenticated API capability that returns the creation months containing non-deleted works for the requested visibility, ordered newest first.
- Preserve the existing `created_from` and `created_to` creator-query contract for compatibility; the Web maps a selected archive month to its inclusive creation-date range.
- Support pointer, keyboard, focus, Escape, outside-dismiss, reduced-motion, loading, empty, and explicit archive-load error states.
- Keep the compact Frux desktop layout within the viewport instead of copying Douyin's narrow-window horizontal overflow.
- Add no frontend runtime dependency and use only the existing typed React, icon, API-client, and CSS systems.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `creator-content-management`: Add creator archive-month discovery and define how an available month constrains the existing creator video query.
- `profile-dashboard`: Replace native date controls with an accessible Douyin-style year/month archive selector that remains usable across Frux desktop densities.

## Impact

- Backend video domain/application interfaces, PostgreSQL creator-query repository, HTTP DTO/handler/router wiring, and creator API-flow or persistence tests.
- Frontend creator API/types, `useCreatorContent`, profile toolbar components, icon registry, profile styles, responsive styles, and focused component/page tests.
- `docs/uiux.md`, `docs/modules/video.md`, and `docs/当前问题.md`.
- No database migration, new table, package dependency, or change to public-profile visibility is expected.
