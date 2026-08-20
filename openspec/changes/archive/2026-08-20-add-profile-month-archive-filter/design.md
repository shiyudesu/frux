## Context

The own-profile work toolbar currently stores independent draft filters for public and private works, then submits `query`, `created_from`, and `created_to` to the existing cursor-paginated creator query. Its two native date inputs inherit browser-specific calendar chrome and consume substantial toolbar width.

Douyin's current desktop profile uses a smaller archive control rather than a calendar: a calendar icon, `日期筛选`, and a chevron open a 225px two-column panel. The left column contains `全部` and descending years; hovering or focusing a year populates its available months in the right column. A selected month replaces the trigger label and filters the work grid.

Frux cannot derive a complete archive from the currently loaded page because creator works are cursor paginated. The backend must therefore expose the distinct creation months that actually contain non-deleted works for the authenticated owner and requested visibility.

Constraints:

- Preserve the existing `created_from` and `created_to` creator-query API.
- Keep public and private work state independent.
- Use PostgreSQL as the source of truth and the existing video repository/application/handler composition.
- Add no frontend date-picker or component dependency.
- Retain Frux keyboard, focus, reduced-motion, explicit-error, and narrow-desktop requirements instead of copying inaccessible or overflowing source behavior.

## Goals / Non-Goals

**Goals:**

- Replace native profile date inputs with a faithful Frux implementation of the Douyin year/month archive interaction.
- Return only months that contain matching owner works for the selected visibility.
- Apply month selection immediately and reset pagination without changing stable creator ordering.
- Preserve keyword filtering and all existing creator-query compatibility.
- Provide complete pointer and keyboard operation at wide, compact, and narrow desktop densities.
- Refresh archive metadata after creator mutations that can add, remove, or move the final work in a month.

**Non-Goals:**

- Removing `created_from` or `created_to` from the creator-query contract.
- Adding arbitrary date-range selection to the new profile control.
- Filtering public profiles or personal-library tabs by month.
- Copying Douyin trademarks, SVG paths, source code, narrow-window overflow, or hover-only accessibility behavior.
- Adding persistent archive tables, caches, or database migrations.
- Changing video lifecycle, visibility, publication time, or creator-query ordering.

## Decisions

### 1. Add a visibility-scoped archive-month endpoint

Add an authenticated read endpoint:

```http
GET /api/users/me/video-archive-months?visibility=public
```

The response is:

```json
{
  "months": ["2026-08", "2026-07", "2025-12"]
}
```

`visibility` is required and uses the existing public/private validation. The authenticated user ID comes only from middleware context. Results contain unique UTC creation months for the owner's non-deleted works with that visibility, ordered newest first.

The video repository will select distinct month starts using the existing `author_id, visibility, created_at, id` access path. Infrastructure returns normalized UTC month values; application/HTTP projection emits canonical `YYYY-MM` strings.

**Alternatives considered:**

- Derive months from loaded Web pages: rejected because pagination makes the archive incomplete.
- Return archive months on every creator-query page: rejected because pagination and load-more requests would repeat the distinct-month query.
- Add a materialized archive table: rejected because creator volumes do not justify new persistence or reconciliation complexity.

### 2. Keep range-query compatibility and translate the selected month in the Web

The Web stores `createdMonth` as `""` or canonical `YYYY-MM`. Before calling the existing creator query it converts the value to:

- `created_from`: the first UTC calendar day of the month as `YYYY-MM-01`
- `created_to`: the last UTC calendar day of the month as `YYYY-MM-DD`

This preserves the current inclusive date contract and avoids changing cursor binding or backend range validation. Pure UTC helpers perform the conversion without local-time rollover.

**Alternatives considered:**

- Add `created_month` to the creator query: rejected because it duplicates the existing range capability and expands the compatibility surface.
- Convert browser-local month boundaries to RFC 3339: rejected for this change because archive discovery and the existing date-only contract are UTC-based; timezone semantics can be changed separately if required.

### 3. Model archive metadata independently per work tab

`useCreatorContent` will maintain archive state for `published` and `private` alongside each tab's video state:

```text
archiveMonths
├── published: items + state + error
└── private:   items + state + error
```

The first activation of a work tab loads its video page and archive months. Archive requests use independent request generations so stale responses cannot update a later session or tab state.

Successful batch visibility/delete operations refresh both video tabs and both archive lists. If a selected month disappears because the last matching work moved or was deleted, that tab resets to `全部` and reloads from its first page. Archive failures remain visible and retryable without replacing successfully loaded videos with a false success state.

**Alternatives considered:**

- Keep archive state inside the picker: rejected because mutations, authentication changes, and tab lifecycle are owned by the creator-content hook.
- Re-fetch archive months on every panel open: rejected because the data changes only after creator mutations in this surface and repeated requests add no value.

### 4. Implement a dedicated controlled profile archive component

Add a focused component such as `ProfileMonthArchiveFilter` rather than embedding calendar logic in `CreatorWorkToolbar`. Its controlled inputs include:

- canonical selected month
- archive months
- loading/error state
- selection, retry, and open-state callbacks as required

The trigger uses a Frux-owned calendar icon, localized label (`日期筛选` or `YYYY年M月`), chevron, `aria-expanded`, and `aria-haspopup="dialog"`.

The panel follows the observed Douyin geometry and hierarchy:

- 225px total width and 292px maximum height
- two 112px scrollable columns separated by a 1px line
- 8px column padding and 46px option rows
- 12px panel radius, raised dark surface, existing Frux shadow tokens
- `全部` plus descending years on the left
- months for the active year on the right
- rose selected text and restrained hover/focus fill

Pointer hover opens the panel and uses a 500ms leave grace period. Click, Enter, Space, or ArrowDown also opens it. Hover or keyboard focus changes the active year. Selecting a year chooses its first available month, matching the source interaction; selecting a month chooses that exact month. `全部` clears the month.

The panel uses dialog/listbox semantics, visible focus, Arrow/Home/End navigation, Left/Right column movement, Escape dismissal with focus restoration, outside-pointer dismissal, and reduced-motion styling. Every internal button uses `type="button"` so the surrounding filter form is not accidentally submitted.

**Alternatives considered:**

- Reuse native month/date inputs: rejected because their popup remains browser-controlled and does not reproduce the archive interaction.
- Use a third-party date picker: rejected because the required control is a simple archive list and the repository permits no unnecessary dependency.
- Use hover only: rejected because it excludes keyboard and touch/pointer users.

### 5. Apply month selections immediately while retaining keyword submission

Selecting `全部`, a year, or a month updates the active tab's draft and immediately calls the creator query with the complete next filter snapshot, including any visible keyword draft. This resets the cursor and closes the panel.

The existing form submit remains available for keyword changes. The implementation may relabel the submit action from generic `筛选` to `搜索` only if the final toolbar wording remains consistent with `docs/uiux.md`; this wording change is not required for the month-picker capability.

### 6. Bound responsive placement inside the Frux profile

Wide desktop anchors the panel below the trigger at the toolbar edge. Compact and narrow desktop keep the same two-column panel but clamp it within `calc(100vw - 24px)` and align it to the nearest safe viewport edge. The toolbar may wrap according to existing profile breakpoints, but the picker must not create document-level horizontal overflow.

No portal is required unless implementation verification shows clipping by a scroll or stacking ancestor. If a portal becomes necessary, placement must use native viewport coordinates and preserve focus/outside-dismiss behavior.

## Risks / Trade-offs

- **[Month granularity removes arbitrary profile ranges]** → Preserve the range API for other clients and explicitly scope the profile control to archive months.
- **[Distinct-month reads add a query]** → Load once per tab, refresh only after relevant mutations, and use the existing creator index and bounded per-user result cardinality.
- **[Archive and video requests can resolve out of order]** → Use independent request generations and validate the active authentication/session context before committing state.
- **[Deleting the final item in a selected month can strand the filter]** → Reconcile refreshed archive data and reset that tab to `全部` when its selection no longer exists.
- **[Hover behavior can be inaccessible or fragile]** → Treat hover as an enhancement; click, focus, keyboard navigation, Escape, and explicit ARIA semantics remain authoritative.
- **[UTC month boundaries may differ from a user's local calendar near midnight]** → Preserve the existing UTC date-only contract in this change and document any future timezone change as a separate API decision.
- **[Floating panel can clip in compact layouts]** → Verify wide, compact, and narrow desktop placements and allow a portal fallback without transforming the shell.

## Migration Plan

1. Add and test the archive-month repository/application/HTTP read path.
2. Deploy the additive endpoint; existing Web clients continue using the current date inputs and creator query.
3. Add the typed Web API, per-tab archive state, month conversion helpers, and custom picker.
4. Replace the native inputs after endpoint support is available.
5. Refresh related UI/module documentation and mark issue 45 resolved after visual and functional validation.

Rollback is additive: revert the Web to native date inputs while leaving the unused archive endpoint in place, or remove the endpoint after all deployed clients stop calling it. No schema or data rollback is required.

## Open Questions

None. The change intentionally adopts UTC creation-month semantics and month-only profile filtering while preserving the existing range API for compatibility.
