## Why

The TypeScript migration is build-clean but its final behavior-preservation task is still unverified because the application has no browser automation. Running the complete workflow through a real browser now is necessary to catch routing, session, API, and interaction regressions before the migration is considered complete.

## What Changes

- Add a repeatable browser smoke-test capability for the web application's critical user journeys.
- Exercise registration/login, all four feed scenes, like/favorite/follow/comment interactions, messages, profile editing, public profiles, work viewing, and upload.
- Treat unexpected console errors, failed API requests, broken routes, and behavior differences as test failures.
- Fix migration regressions discovered by the browser run and repeat affected scenarios.
- Record the completed migration smoke verification in the existing migration task list.

## Capabilities

### New Capabilities

- `web-browser-smoke-testing`: Defines real-browser verification coverage, failure criteria, test data handling, and completion evidence for critical web journeys.

### Modified Capabilities

None.

## Impact

- **Web application**: `apps/web/src/**` may receive targeted regression fixes.
- **Runtime environment**: Local API, worker, web server, MySQL, Redis, and RabbitMQ are exercised together.
- **Tooling**: Playwright MCP controls an isolated headless browser; no new application runtime dependency is required.
- **OpenSpec**: The remaining `migrate-web-to-typescript` smoke-test task is completed only after all required browser scenarios pass.
