# Design: migrate-web-to-pnpm

## Context

`apps/web` is a small React 18 + Vite 5 app (3 source files, 4 direct dependencies) managed with npm. The repo root has no `package.json` — `apps/web` is a standalone npm project, not a workspace. Backend is Go (`apps/api`) and unaffected. pnpm 10.33.2 is already installed on dev machines; Node 22 is the baseline (matches `node:22-alpine` in the Dockerfile, which ships Corepack).

npm touchpoints found by repo-wide search:
- `apps/web/package-lock.json` (lockfile)
- `apps/web/Dockerfile` (`npm ci`, `npm run build`)
- `scripts/start.sh` (npm presence check, `npm run dev`)
- `README.md:110` (`npm --prefix apps/web run build`)
- `docs/engineering.md:324` (`npm run build`)
- `openspec/project.md:59` (`cd apps/web && npm run build`)

`apps/deploy.yaml` (k8s, only `nodePort` — unrelated) and `apps/monitoring` (Prometheus/Grafana config) have no npm usage.

## Goals / Non-Goals

**Goals:**
- `apps/web` installs, builds, and develops with pnpm only; `pnpm-lock.yaml` is the single lockfile.
- Docker image build produces identical output (static `dist/` served by nginx).
- All docs and scripts reference pnpm.

**Non-Goals:**
- TypeScript migration (separate future change).
- Introducing a pnpm workspace / root `package.json` — single package, no need yet.
- Changing any dependency versions or application source.
- Migrating CI pipelines (none exist in-repo besides Dockerfile/scripts).

## Decisions

### D1: No pnpm workspace — migrate `apps/web` in place
Alternatives: (a) add root `pnpm-workspace.yaml` and hoist `apps/web` into it. Rejected: only one JS package exists; the Go backend will never join. A workspace adds config (`pnpm-workspace.yaml`, root lockfile location changes, Dockerfile COPY paths change) for zero current benefit. If a second JS package ever appears, introducing the workspace is a small follow-up.

### D2: Pin pnpm via `packageManager` field + Corepack
Add `"packageManager": "pnpm@10.33.2"` to `apps/web/package.json`. Node 22 ships Corepack, so both local dev and Docker get the exact same pnpm version without a global install step.
- Local: developers with pnpm already installed are unaffected; Corepack shims handle the rest.
- Docker: `RUN corepack enable` then `pnpm install --frozen-lockfile`. Set `COREPACK_ENABLE_DOWNLOAD_PROMPT=0` to keep builds non-interactive.
Alternative considered: `npm install -g pnpm` in Dockerfile — rejected, it bypasses version pinning and adds an npm dependency we're trying to remove.

### D3: Regenerate lockfile from existing package.json, no version bumps
Run `pnpm install` in `apps/web` to generate `pnpm-lock.yaml`, then delete `package-lock.json`. Dependency ranges are unchanged, so the resolution risk is limited to transitive versions drifting within existing semver ranges — acceptable for a 4-dependency dev-only toolchain, and verified by building.
Optional hardening (do it): `pnpm import` reads `package-lock.json` and preserves exact resolved versions in the new `pnpm-lock.yaml`. Prefer `pnpm import` over a fresh resolve.

### D4: Dockerfile changes are minimal and layer-cache friendly
Current: `COPY package*.json ./ && npm ci && COPY . . && npm run build`.
New: `COPY package.json pnpm-lock.yaml ./ && corepack enable && pnpm install --frozen-lockfile && COPY . . && pnpm run build`. Layer ordering (manifest+lockfile before source COPY) is preserved. `--frozen-lockfile` is the pnpm equivalent of `npm ci`.

### D5: Scripts and docs get mechanical command swaps
- `scripts/start.sh`: `command -v npm` → `command -v pnpm`, `npm run dev` → `pnpm run dev`.
- `README.md`: `npm --prefix apps/web run build` → `pnpm --dir apps/web run build` (or `pnpm -C apps/web build`).
- `docs/engineering.md`, `openspec/project.md`: `npm run build` → `pnpm run build` in the `apps/web` context.

## Risks / Trade-offs

- [Transitive dependency drift if lockfile is regenerated freely] → Use `pnpm import` from `package-lock.json` to pin exact versions; verify with `pnpm run build` before deleting the old lockfile.
- [Corepack network fetch in Docker build (it downloads pnpm on first use)] → Pin version in `packageManager`; Corepack caches after first fetch; acceptable for current build infra. If offline builds become a requirement, vendor pnpm via `corepack pack` later.
- [Developer muscle memory / mixed tooling (someone runs `npm install` and recreates `package-lock.json`)] → `packageManager` field makes Corepack error on wrong PM; optionally add `.npmrc` or a note in README. No hard enforcement (e.g., `only-allow` preinstall hook) — judged overkill for team size.
- [`pnpm`'s non-flat `node_modules` breaks phantom dependencies] → The app has 4 direct deps and imports only `react`, `react-dom`, `vite`; build verification catches any issue.

## Migration Plan

1. `cd apps/web && pnpm import` (generates `pnpm-lock.yaml` from `package-lock.json`).
2. Verify: `pnpm install --frozen-lockfile && pnpm run build` succeeds; `pnpm run dev` smoke test.
3. Delete `package-lock.json`; add `packageManager` field.
4. Update Dockerfile, `scripts/start.sh`, README, engineering.md, project.md.
5. Verify Docker build: `docker build apps/web` (or note as manual verification step if Docker unavailable).
Rollback: restore `package-lock.json` from git and revert the edits — no stateful changes involved.

## Open Questions

- None blocking. (Docker daemon availability for step 5 verification is environment-dependent; worst case, the build is verified on next deploy.)
