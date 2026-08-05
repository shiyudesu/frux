## Why

Review and content-enforcement APIs need a focused operator experience, but placing all platform configuration and reliability controls into one generic admin module would create an oversized ownership boundary. This change adds only the content and review workspace.

## What Changes

- Add typed admin Web routes and a permission-aware internal navigation shell.
- Provide review queue, review detail, evidence, assignment, and decision screens over the human-review APIs.
- Add cursor-paginated admin video search by status, author, identifier, and creation window.
- Allow permitted operators to take videos offline or restore eligible videos with required reason codes and audit attribution.
- Present truthful loading, empty, conflict, lease-expired, forbidden, and retry states.
- Exclude generic configuration editing, user administration, monitoring dashboards, degradation controls, and dead-letter replay.

## Capabilities

### New Capabilities

- `content-operations-console`: Typed internal Web workspace for review and content operations.

### Modified Capabilities

- `web-frontend`: The typed hand-written router and API boundary gain permission-aware admin routes without adding a routing library.

## Impact

This affects Web routing, session permission types, API modules, admin pages/components/styles, video management queries and handlers, audit integration, tests, and UI/product/module documentation. It depends on the authorization, audit, lifecycle, and human-review changes.
