## Context

The existing Web app uses a strict TypeScript History API router and shared session context. The admin workspace should reuse that stack rather than introduce a second frontend or routing library. Backend authorization remains authoritative; client permissions only control navigation and affordances.

## Goals / Non-Goals

**Goals:**

- Add a small admin shell for review work and video operations.
- Preserve typed routing, API boundaries, and explicit page states.
- Provide searchable content operations without making an admin module own video or review data.
- Require reasoned, audited enforcement actions.

**Non-Goals:**

- Generic configuration, user administration, monitoring, governance, DLQ, or recommendation policy pages.
- Replacing the public Web layout or router.
- Client-side authorization as a security boundary.

## Decisions

### Extend the existing typed router

Add typed routes for `/admin/reviews`, `/admin/reviews/:reviewId`, and `/admin/videos`. Route normalization rejects invalid IDs and redirects unauthorized users to a stable forbidden state without losing the public session.

### Build one shell with permission-filtered navigation

The shell renders only destinations allowed by the current principal. Direct navigation still calls backend APIs, which return the authoritative `403`.

### Keep domain API modules separate

Review operations live in a review API module; video search and enforcement live in a video-admin API module. The UI composes them but does not introduce a generic admin configuration client.

### Use server-side stable pagination and filters

Video search orders by `created_at DESC, id DESC` and binds status, author, identifier, keyword, and time filters into the cursor. Review pages use the queue cursor defined by the human-review capability.

### Require explicit confirmation only for enforcement

Taking offline or restoring a video requires a reason code, optional bounded note, and confirmation. Review decisions use the dedicated case detail workflow and lease token.

### Deliver enforcement side effects from a transactional intent

The video transition transaction writes one durable intent with the lifecycle, content-stat, enforcement, and audit facts. A bounded Worker leases intents with `SKIP LOCKED`, invalidates public caches, converges media protection/publication from the current video state, and marks delivery only after every side effect succeeds. Failures retain the intent with bounded error text and exponential retry.

### Retain pending review decision identity

The review detail page derives one pending decision signature from the case and normalized decision payload. Retries after response loss reuse its idempotency key; success or any case/payload change creates a new identity.

## Risks / Trade-offs

- [Admin code increases the public bundle] -> Use route-level lazy imports within the existing Vite build.
- [Client permissions become stale] -> Treat them as presentation hints and surface backend forbidden responses truthfully.
- [Operators act on stale cases] -> Carry expected versions and lease tokens and show conflict recovery instead of overwriting.
- [Large search filters become inconsistent] -> Normalize them in typed helpers and bind them into cursors.

## Migration Plan

1. Add permission types, routes, and an inaccessible shell behind no navigation entry.
2. Add typed API modules and page tests with fake responses.
3. Enable navigation for authorized principals after backend changes are available.
4. Roll back by removing the admin navigation and route dispatch; backend APIs remain independently usable.

## Open Questions

- Whether the production deployment should later serve the admin shell from a separate origin for stronger network isolation.
