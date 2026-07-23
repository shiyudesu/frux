# web-package-management Specification (delta for migrate-web-to-typescript)

## MODIFIED Requirements

### Requirement: Docker image builds with pnpm

The `apps/web` Dockerfile SHALL install dependencies and build the frontend with pnpm (provisioned via Corepack on the Node base image), producing the same static `dist/` output served by nginx as before. Because `pnpm run build` now runs `tsc --noEmit` before `vite build`, the image build SHALL fail when the frontend contains TypeScript type errors.

#### Scenario: Image build succeeds

- **WHEN** the `apps/web` Docker image is built
- **THEN** the build uses `pnpm install --frozen-lockfile` and `pnpm run build`, and the resulting image serves the built frontend via nginx

#### Scenario: Image build fails on type errors

- **WHEN** the `apps/web` Docker image is built while `apps/web/src` contains a TypeScript type error
- **THEN** `pnpm run build` fails at the `tsc --noEmit` step and no image serving stale or untyped output is produced
