## Context

Frux uses a fixed desktop shell: `body`, `.app-shell`, and `.app-body` suppress document scrolling, so each routed page must own a height-constrained scroll container. Profile, messages, upload, and video-detail routes establish `height: 100%`; the search route instead uses only `min-height: 100%`.

When search content exceeds the viewport, `.search-page` can expand to its content height rather than overflow its own box. `.app-body` then clips that expansion with `overflow: hidden`. The API may have returned a complete 20-item page and `has_more=true`, while users can see only the viewport-sized prefix and cannot reach the load-more button below it.

## Goals / Non-Goals

**Goals:**

- Make the search route the vertical scroll owner inside the existing fixed shell.
- Keep every returned result, error state, and pagination control reachable.
- Verify cursor continuation across multiple Web pages for video and user searches.
- Detect regressions with both deterministic component tests and real-browser layout validation.

**Non-Goals:**

- Changing backend search limits, ranking, SQL, cursor encoding, or response shapes.
- Replacing explicit load-more controls with infinite scrolling.
- Changing the fixed desktop shell or enabling document-level scrolling.
- Combining this defect with the account-identifier privacy change.

## Decisions

### 1. Constrain the search route to the available shell height

The search page will use the same route-container contract as other scrollable pages: a definite `height: 100%`, `min-height: 0`, and `overflow-y: auto`.

`min-height: 0` is included so the route may shrink within a constrained flex or grid ancestor if the shell layout evolves. Keeping scrolling on `.search-page` avoids moving header or side-navigation behavior and preserves the current fixed-shell architecture.

Document scrolling and changing `.app-body` to `overflow: auto` were rejected because they would alter every route and could introduce nested or competing scroll behavior in Feed and profile surfaces.

### 2. Preserve explicit cursor pagination

The existing `useSearch` state and API functions remain the pagination boundary:

```text
items + next_cursor + has_more
              |
              v
       visible load-more
              |
              v
 cursor request + deduplicated append
```

The fix does not increase the first-page limit to hide the layout problem. A larger page would still be clipped and would increase response and rendering cost.

### 3. Test state behavior separately from browser geometry

Hook tests will prove that a first page with `has_more=true` retains its cursor and that loading again passes that cursor, appends unique items, and reaches the terminal page.

Page tests will prove that the load-more action is rendered and invokes continuation. Because jsdom does not perform CSS layout, a Chrome browser check at a bounded viewport will verify that the search page scrolls and the pagination control becomes reachable below a tall result set.

## Risks / Trade-offs

- **[Nested scroll regression]** A second ancestor could also become scrollable later. → Keep `.app-body` fixed and assert the search page is the route scroll owner in browser validation.
- **[Layout test false confidence]** jsdom cannot detect clipping. → Require a real Chrome viewport check in addition to component tests.
- **[Pagination state bug remains hidden]** Fixing CSS alone could expose an existing continuation defect. → Add explicit two-page hook and page interaction tests.
- **[Unrelated routes change]** Broad shell CSS edits could affect Feed or profile behavior. → Make the CSS change specific to `.search-page`.

## Migration Plan

1. Add the route-height and overflow correction for `.search-page`.
2. Add multi-page state and page interaction regression tests.
3. Run the focused frontend test selectors and production build.
4. Start the Web app with representative search data and validate scrolling and continuation in Windows Chrome at a desktop viewport.

Rollback is a CSS and test revert with no data or API migration.

## Open Questions

None.
