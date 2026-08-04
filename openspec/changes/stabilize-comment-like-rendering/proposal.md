## Why

Comment likes already use optimistic local state, but each mutation replaces the shared comment entity map and controller object, causing the entire visible comment tree to repaint. Users perceive this as the whole comment list refreshing even though no list request is made.

## What Changes

- Isolate comment-like mutation state so only the affected comment card needs to render again.
- Preserve loaded roots, expanded replies, drafts, focus, and scroll position while a comment like is pending, succeeds, or rolls back.
- Add regression coverage proving comment likes do not remount or visually reset unaffected comment threads.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `threaded-comments`: Require comment-like updates to remain local to the target comment while preserving the surrounding discussion state.

## Impact

- Affects `apps/web/src/hooks/useComments.ts`, `ThreadedComments`, comment component boundaries, and focused frontend tests.
- Does not change comment APIs, persistence, counters, sorting, or authentication behavior.
