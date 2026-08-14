## Why

Search APIs already return stable cursor pages, but the Web search route can grow beyond its fixed-height shell without becoming the active scroll container. The shell then clips later results and the load-more control, making a complete first page look truncated and preventing users from requesting subsequent pages.

## What Changes

- Make the search page a height-constrained, independently scrollable route within the existing fixed desktop shell.
- Ensure all returned video and user results, inline errors, and pagination controls remain reachable by scrolling.
- Add multi-page Web state and page-level regression coverage so cursor continuation is exercised rather than inferred from single-item tests.
- Add browser-level validation for a result set taller than the viewport.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `global-search`: Require search results and load-more controls to remain reachable inside the fixed application shell and verify continuation through multiple cursor pages.

## Impact

- Affected Web code: search page layout CSS, search page tests, search state tests, and browser smoke coverage.
- Search API contracts, cursor encoding, backend persistence queries, ranking, and page size remain unchanged.
- No database migration, dependency change, or breaking API change is required.
