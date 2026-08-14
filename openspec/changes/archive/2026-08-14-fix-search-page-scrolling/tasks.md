## 1. Restore Search Route Scrolling

- [x] 1.1 Update the search page route container to use the available application-body height, allow shrinking, and own vertical overflow without changing the fixed shell or other routes.
- [x] 1.2 Confirm video grids, user rows, inline errors, and load-more controls remain inside the search scroll container at compact and wide desktop widths.

## 2. Add Pagination Regression Coverage

- [x] 2.1 Extend `useSearch` tests with a two-page video case that verifies the next cursor is sent, unique items are appended, and terminal has-more state is retained.
- [x] 2.2 Extend `useSearch` tests with a two-page user case that verifies independent cursor and item state from the video tab.
- [x] 2.3 Extend search page tests to render a tall first page with `has_more=true`, expose the load-more action, invoke it, and render the appended page without replacing existing results.

## 3. Validate Real Layout and Documentation

- [x] 3.1 Run the focused search hook and page tests followed by `pnpm -C apps/web run build`.
- [x] 3.2 Start the Web stack with enough matching user or video fixtures to exceed one page, then use Windows Chrome at a desktop viewport to verify the search route scrolls to all 20 first-page items and the load-more control.
- [x] 3.3 Verify loading the next cursor page appends results, preserves category state, and leaves the next or terminal pagination control reachable.
- [x] 3.4 Update `docs/modules/search.md` to state that the search route owns vertical scrolling inside the fixed shell and exposes reachable explicit cursor pagination.
- [x] 3.5 Run `openspec validate --all --strict` and resolve every validation error.
