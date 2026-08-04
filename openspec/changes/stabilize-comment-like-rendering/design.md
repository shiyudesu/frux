## Context

`useComments` stores every comment entity and every per-video operation in one normalized React state object. A like mutation correctly updates only one entity semantically, but it also creates a new entity map, video state, controller object, root array, and controller prop for every visible thread. Because `CommentThread` and `CommentCard` are not memoized around stable, narrow props, React renders the full discussion subtree and users see a list-wide flash.

The backend request, optimistic update, exact rollback, counters, and viewer-generation guards are already correct. This change is a frontend render-boundary correction rather than a data-flow rewrite.

## Goals / Non-Goals

**Goals:**

- Render the target comment and its containing thread when its like state changes without repainting unrelated threads.
- Keep the current optimistic response, rollback, authentication, loaded pages, expansion, draft, focus, and scroll behavior.
- Add deterministic regression coverage for the render boundary.

**Non-Goals:**

- Changing comment APIs, persistence, hot-score calculation, pagination, or sorting.
- Introducing an external state library or React context-selector dependency.
- Refactoring unrelated comment creation or deletion behavior.

## Decisions

### Use narrow memoized thread and card props

`ThreadedComments` will stop passing the complete `CommentsController` through every thread. It will pass stable callbacks plus the specific root, replies, list state, and operation state needed by each subtree. `CommentThread` and `CommentCard` will be memoized so unchanged comment object references and unchanged operation objects skip rendering.

This retains the existing normalized store and avoids a new subscription framework. A per-entity external store was considered but rejected as disproportionate for the current comment surface.

### Preserve entity object identity for unaffected comments

Like success, optimistic mutation, and rollback will continue replacing only the target comment object. Unaffected entries in the entity map MUST retain their object references, and per-video operation maps MUST replace only the target operation entry.

### Test committed render behavior

Frontend tests will render multiple threads, perform a like on one comment, and record render counts or equivalent committed updates for cards. The target card may render for optimistic and server-confirmed states; unrelated cards must not render because of the mutation. Tests will also assert that loaded/expanded state and scroll container position remain stable.

## Risks / Trade-offs

- [Custom memo comparison can hide legitimate updates] → Keep props explicit, compare object references rather than selected scalar subsets, and cover content, deletion, expansion, and reply changes in tests.
- [The parent list still executes its mapping function] → Accept the small reconciliation cost; the user-visible and expensive card/thread subtrees are the optimization boundary.
- [Development Strict Mode can invoke render functions more than once] → Assert relative committed updates or compare target versus unaffected components rather than fixed global render totals.

## Migration Plan

No data or API migration is required. The change can ship as a frontend-only update and can be rolled back by restoring the prior component prop boundary.

## Open Questions

None.
