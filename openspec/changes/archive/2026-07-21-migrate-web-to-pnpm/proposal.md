# Proposal: migrate-web-to-pnpm

## Why

The `apps/web` frontend currently uses npm (`package-lock.json`, `npm ci` in Dockerfile, npm commands in scripts and docs). The team wants pnpm as the standard package manager for the frontend: faster installs via content-addressable store, stricter dependency resolution, and a lockfile that matches the local tooling (pnpm 10 is already installed on dev machines). TypeScript migration is explicitly out of scope for this change.

## What Changes

- Replace `apps/web/package-lock.json` with `pnpm-lock.yaml` generated from the existing `package.json` (no dependency version changes).
- Pin the package manager in `apps/web/package.json` via the `packageManager` field (`pnpm@10.x`).
- Update `apps/web/Dockerfile` to install dependencies and build with pnpm (via Corepack on `node:22-alpine`), keeping the multi-stage nginx output unchanged.
- Update npm references to pnpm in `scripts/start.sh`, `README.md`, `docs/engineering.md`, and `openspec/project.md`.
- No changes to application source code, dependency versions, or the Go backend.

## Capabilities

### New Capabilities
- `web-package-management`: Defines that the `apps/web` frontend uses pnpm as its package manager, including lockfile, install/build commands, and Docker build behavior.

### Modified Capabilities
<!-- None: platform-basics requirements (verification commands, engineering conventions) remain satisfied after docs are updated; they do not name npm. -->

## Impact

- **Code**: none (no source changes).
- **Build/Tooling**: `apps/web/package.json`, `apps/web/pnpm-lock.yaml` (new), `apps/web/package-lock.json` (removed), `apps/web/Dockerfile`, `scripts/start.sh`.
- **Docs**: `README.md`, `docs/engineering.md`, `openspec/project.md`.
- **Dependencies**: identical versions; only the lockfile format changes.
- **Risk**: lockfile regeneration must preserve resolved versions; Docker build must not require network access to Corepack beyond fetching pnpm (mitigated by pinning via `COREPACK_ENABLE_DOWNLOAD_PROMPT=0` and `corepack prepare`).
