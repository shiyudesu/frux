# Tasks: migrate-web-to-pnpm

## 1. Lockfile & package.json

- [x] 1.1 In `apps/web`, run `pnpm import` to generate `pnpm-lock.yaml` from the existing `package-lock.json` (preserves exact resolved versions)
- [x] 1.2 Add `"packageManager": "pnpm@10.33.2"` to `apps/web/package.json` (no dependency version changes)
- [x] 1.3 Verify locally: `pnpm install --frozen-lockfile && pnpm run build` succeeds in `apps/web`, then smoke-test `pnpm run dev`
- [x] 1.4 Delete `apps/web/package-lock.json`

## 2. Docker

- [x] 2.1 Update `apps/web/Dockerfile`: replace `COPY package*.json` with `COPY package.json pnpm-lock.yaml ./`, add `corepack enable` (with `COREPACK_ENABLE_DOWNLOAD_PROMPT=0`), replace `npm ci` with `pnpm install --frozen-lockfile` and `npm run build` with `pnpm run build`
- [x] 2.2 Verify the Docker image builds (`docker build apps/web`) or document it as a manual verification step if no Docker daemon is available

## 3. Scripts & Docs

- [x] 3.1 Update `scripts/start.sh`: check for `pnpm` instead of `npm`, run `pnpm run dev` in `apps/web`
- [x] 3.2 Update `README.md:110`: `npm --prefix apps/web run build` → `pnpm -C apps/web run build`
- [x] 3.3 Update `docs/engineering.md:324`: web build command → `pnpm run build`
- [x] 3.4 Update `openspec/project.md:59`: web verification command → `pnpm run build`

## 4. Final Verification

- [x] 4.1 Repo-wide search confirms no remaining npm references outside git history (excluding unrelated `nodePort`/`node_modules` matches)
- [x] 4.2 Clean-checkout verification: fresh `pnpm install --frozen-lockfile && pnpm run build` in `apps/web` passes
