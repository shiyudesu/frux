## Context

The `migrate-web-to-typescript` change has passed strict type checking and production builds, but its final six-page smoke checklist remains incomplete. GCFeed is a multi-service application, so a meaningful browser test must run against the real API and supporting infrastructure rather than a mocked frontend.

The browser driver is Playwright MCP in an isolated headless context. The existing application does not have an end-to-end test framework, and this verification must not introduce a new runtime dependency or alter production behavior.

## Goals / Non-Goals

**Goals:**

- Verify every critical web page and interaction required by the TypeScript migration.
- Detect failures through visible state, route changes, API responses, browser console messages, and failed network requests.
- Create isolated test users and content so the run does not depend on pre-existing accounts.
- Fix migration regressions and repeat the affected flow until it passes.
- Leave the repository build-clean and mark the migration smoke task complete only with full evidence.

**Non-Goals:**

- Establish a permanent CI end-to-end test suite in this change.
- Benchmark frontend or backend performance.
- Redesign user experience or change backend contracts.
- Treat unrelated infrastructure availability problems as frontend migration regressions.

## Decisions

### D1: Test the integrated local stack

The smoke run uses the real web server, API, worker, database, cache, and queue. Mocking API responses was rejected because it would miss request-shape, authentication, persistence, and route-integration regressions introduced during migration.

### D2: Use isolated browser state and uniquely named test data

Playwright runs with an in-memory isolated profile. Test account names and uploaded content use a unique run suffix. This avoids dependence on stored login state and minimizes collisions across repeated runs.

### D3: Verify by user-visible outcomes plus runtime diagnostics

Each scenario requires the expected page or interaction result. The run also checks console errors and failed HTTP requests, because a superficially rendered page can still hide migration defects in effects, event handlers, or background API calls.

### D4: Cover workflows in dependency order

Registration/login runs first, followed by upload to create owned content, profile/work viewing, feeds and interactions, messages, and logout/login persistence. This ordering creates the data required by later scenarios through supported application flows.

### D5: Fix only reproducible migration regressions

When a scenario fails, the failure is reproduced and traced to the current TypeScript changes before editing. Infrastructure or seed-data issues are documented separately rather than hidden with frontend fallbacks.

## Risks / Trade-offs

- [Local infrastructure cannot start or is unhealthy] → Validate services and logs before browser testing; repair only repository-owned startup defects.
- [Feed ranking does not immediately surface newly uploaded content] → Verify owned content through profile/work viewing and use available feed data for interaction coverage.
- [Asynchronous message generation is delayed] → Wait for the worker-backed result with a bounded timeout and verify the triggering interaction separately.
- [Headless media playback differs from a desktop browser] → Verify video element presence, source loading, controls, and navigation; do not require audio output.
- [Test data persists locally] → Use identifiable unique names and avoid destructive cleanup that could affect existing developer data.

## Migration Plan

1. Start and health-check the complete local stack.
2. Create isolated test data and execute all specified browser scenarios.
3. Fix and repeat any reproducible migration regressions.
4. Run the frontend build and strict OpenSpec validation.
5. Mark `migrate-web-to-typescript` task 6.4 complete and preserve the browser-verification result in this change.

Rollback is limited to reverting targeted regression fixes and the verification artifacts; the browser run itself does not change production configuration.

## Open Questions

None.
