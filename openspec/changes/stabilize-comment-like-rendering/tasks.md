## 1. Comment Render Boundaries

- [x] 1.1 Refactor `ThreadedComments` so thread and card components receive narrow comment, operation, list, and stable callback props instead of the complete changing controller.
- [x] 1.2 Memoize comment thread/card boundaries and preserve object identity for unaffected entities and operation entries during optimistic like, confirmation, and rollback updates.

## 2. Regression Coverage

- [x] 2.1 Add frontend tests that like and unlike one root or reply and prove unrelated visible comment cards do not commit new renders.
- [x] 2.2 Cover failed-like rollback while preserving loaded roots, expanded replies, draft, focus, and scroll state.

## 3. Documentation and Validation

- [x] 3.1 Update the interaction frontend documentation to describe local optimistic comment-like rendering and stable discussion state.
- [x] 3.2 Run the targeted comment hook/component tests and the frontend production/type-check build.
