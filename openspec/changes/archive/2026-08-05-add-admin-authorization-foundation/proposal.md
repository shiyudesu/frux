## Why

Frux currently distinguishes only `user` and `admin`, which would give every privileged user the same authority over review decisions, content enforcement, configuration, and runtime controls. The planned internal control plane needs a small authorization foundation before any admin-facing capability is exposed.

## What Changes

- Introduce named admin permissions for review reading and decisions, content enforcement, configuration publishing, governance execution, and audit reading.
- Add a shared authorization boundary for `/api/admin` handlers that evaluates authenticated permissions instead of repeating role checks.
- Preserve the existing `admin` role as a compatibility role that receives the initial permission set.
- Define consistent forbidden responses and tests for missing, insufficient, and compatible admin authority.
- Exclude operator management UI, external identity providers, organization tenancy, and policy engines from this change.

## Capabilities

### New Capabilities

- `admin-authorization`: Fine-grained permission evaluation and protected admin API behavior.

### Modified Capabilities

None.

## Impact

This affects account role representation, JWT/session claims, HTTP middleware, router composition, admin API tests, and the account/admin module documentation. It is a prerequisite for later review, operations, audit, and runtime-governance changes.
