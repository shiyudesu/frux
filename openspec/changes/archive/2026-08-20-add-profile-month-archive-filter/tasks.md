## 1. Backend Archive-Month Capability

- [x] 1.1 Add a creator archive-month repository/application contract that accepts the authenticated author and validated visibility and returns unique normalized UTC month starts newest first.
- [x] 1.2 Implement the PostgreSQL distinct-month query for non-deleted owned works using the existing creator visibility/creation index and cover public, private, duplicate-month, ordering, and empty-result cases with persistence tests.
- [x] 1.3 Add the authenticated `GET /api/users/me/video-archive-months` DTO, handler, error mapping, and router registration without accepting an author ID from the client.
- [x] 1.4 Add API-flow coverage for public/private month lists, invalid visibility, owner isolation, empty archives, and explicit repository failure.

## 2. Typed Web Data and Filter State

- [x] 2.1 Add canonical archive-month response/request types and a typed creator API function, including runtime-safe response handling and API tests.
- [x] 2.2 Add pure helpers that validate `YYYY-MM`, format `YYYY年M月`, group available months by descending year, and convert a month to inclusive UTC `created_from`/`created_to` dates with boundary tests.
- [x] 2.3 Extend `useCreatorContent` with independent public/private archive items, loading/error state, request generations, lazy loading, and explicit retry behavior.
- [x] 2.4 Change creator profile filter state from two date strings to a canonical selected month while preserving per-tab keyword drafts and mapping the month through the existing range-query request.
- [x] 2.5 Refresh both archive lists after successful batch visibility/delete mutations and reset/reload a tab to `全部` when its selected month no longer exists.
- [x] 2.6 Add hook/page tests for stale archive responses, tab isolation, immediate month application, keyword preservation, mutation refresh, invalidated-month reset, and archive failure without video-grid loss.

## 3. Douyin-Style Archive Selector

- [x] 3.1 Add an original Frux calendar icon to the typed icon registry without copying source-site SVG paths or introducing an icon dependency.
- [x] 3.2 Implement a controlled `ProfileMonthArchiveFilter` with the `日期筛选`/localized-month trigger, `全部`, descending years, active-year months, selected state, and year-to-first-month behavior.
- [x] 3.3 Implement pointer hover with a bounded leave delay, click/touch opening, outside dismissal, Escape focus restoration, and reduced-motion behavior.
- [x] 3.4 Implement dialog/listbox semantics and complete keyboard navigation across year and month columns with visible focus and non-submitting internal buttons.
- [x] 3.5 Style the 225px two-column dark panel, 46px rows, divider, rose selection, hover/focus fill, shadow, and trigger states using existing Frux tokens.
- [x] 3.6 Integrate the selector into `CreatorWorkToolbar`, remove both native date inputs, apply month selections immediately with the full current filter snapshot, and retain form submission for keyword changes.
- [x] 3.7 Clamp and align the panel at wide, compact, and narrow desktop widths so toolbar wrapping and the floating selector do not create document-level horizontal overflow.
- [x] 3.8 Add component interaction tests covering loading, empty, error/retry, hover grace, selection, clear, year/month navigation, outside click, Escape, focus return, and keyboard operation.

## 4. Documentation and Verification

- [x] 4.1 Update `docs/uiux.md` and `docs/modules/video.md` with the month archive interaction, endpoint contract, state behavior, UTC month semantics, and responsive/accessibility rules.
- [x] 4.2 After implementation and browser verification, mark issue 45 resolved in `docs/当前问题.md` with a concise description of the custom year/month archive filter.
- [x] 4.3 Run targeted Go domain/application/persistence/API tests for archive months, then run `cd apps/api && go test ./...`.
- [x] 4.4 Run targeted Web API/hook/component/page tests, then run `pnpm -C apps/web run build`.
- [x] 4.5 Validate the implemented profile at wide, compact, and narrow desktop viewports with Windows Chrome, including open/selected/error states and keyboard focus behavior.
- [x] 4.6 Run `openspec validate --all --strict` and confirm the change remains apply-ready with all implementation evidence linked to the requirements.
