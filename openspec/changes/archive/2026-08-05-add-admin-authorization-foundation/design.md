## Context

Frux stores one role string on the account and copies it into a 15-minute JWT. Only comment moderation currently checks `admin`; the planned admin surface will contain materially different privileges. Admin traffic is low enough to prefer current authorization over a JWT-only decision, and the first version does not need an external policy engine.

## Goals / Non-Goals

**Goals:**

- Define a closed permission registry and a small compatible role-to-permission mapping.
- Make every `/api/admin` handler declare and enforce one required permission.
- Reject disabled or demoted accounts without waiting for an access token to expire.
- Keep authorization reusable by later review, audit, operations, and governance changes.

**Non-Goals:**

- Operator provisioning UI, SSO, organizations, delegated administration, ABAC, or OPA/OpenFGA.
- Changing ordinary authenticated or internal-token endpoints.
- Persisting arbitrary permission expressions.

## Decisions

### Rehydrate admin authority on each privileged request

JWT authentication continues to establish user identity, but admin authorization loads the current account status and role through a narrow `AdminPrincipalReader`. This prevents a demoted or disabled operator from retaining authority until token expiry.

Alternative: trust permissions embedded in the JWT. Rejected because revocation latency is unacceptable for production controls.

### Keep a closed role and permission registry in the domain

The first registry contains `review.read`, `review.decide`, `content.enforce`, `config.publish`, `governance.execute`, and `audit.read`. Initial roles are `reviewer`, `operator`, and the compatible `admin`; `admin` receives all registered permissions.

Alternative: add role-permission tables immediately. Rejected because no role-management workflow exists yet and mutable policy data would enlarge this change.

### Enforce permissions in shared middleware

Router groups attach authentication first and a parameterized permission middleware second. Handlers may use the resolved principal for attribution but MUST NOT replace the middleware with ad hoc role comparisons.

### Return stable authorization errors

Missing or invalid authentication remains `401`. An authenticated principal without the required permission receives `403` with a stable code that does not reveal which higher privilege would satisfy the request.

## Risks / Trade-offs

- [Admin requests add a database read] -> Keep the reader narrow, indexed by account ID, and allow a short bounded cache only after revocation tests exist.
- [Role strings can drift between code and stored rows] -> Unknown roles resolve to no admin permissions and emit a bounded metric.
- [Compatibility admin remains broad] -> Treat it as a bootstrap role and require later operator assignment to use narrower roles.

## Migration Plan

1. Add new role constants and the permission registry without changing existing users.
2. Add principal reading and middleware tests.
3. Protect a test-only or first admin endpoint with the middleware.
4. Preserve existing `admin` rows as full-permission principals.
5. Roll back by removing admin route registration; ordinary JWT authentication remains compatible.

## Open Questions

- Whether production operator provisioning should later be database-managed or delegated to an external identity provider.
