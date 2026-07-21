# web-package-management Specification

## Purpose

Defines how the `apps/web` frontend manages its JavaScript dependencies: pnpm as the only package manager, a single pnpm lockfile, reproducible installs, and pnpm-based Docker builds.

## Requirements

### Requirement: pnpm as the web package manager

The `apps/web` frontend SHALL use pnpm as its only package manager. `apps/web/package.json` SHALL declare the exact pnpm version via the `packageManager` field, and `pnpm-lock.yaml` SHALL be the only lockfile in `apps/web`.

#### Scenario: Developer installs dependencies locally

- **WHEN** a developer runs `pnpm install` in `apps/web`
- **THEN** dependencies are installed from `pnpm-lock.yaml` and no `package-lock.json` is created or required

#### Scenario: Wrong package manager is used

- **WHEN** a developer runs the install through Corepack with a package manager that does not match the `packageManager` field
- **THEN** Corepack rejects the command instead of silently installing with the wrong tool

### Requirement: Reproducible dependency installation

`apps/web` SHALL support reproducible installs via `pnpm install --frozen-lockfile`, equivalent to the previous `npm ci` behavior, without changing declared dependency versions.

#### Scenario: Frozen install in a clean checkout

- **WHEN** a clean checkout of the repo runs `pnpm install --frozen-lockfile` in `apps/web`
- **THEN** installation succeeds using only the pinned versions in `pnpm-lock.yaml`

### Requirement: Docker image builds with pnpm

The `apps/web` Dockerfile SHALL install dependencies and build the frontend with pnpm (provisioned via Corepack on the Node base image), producing the same static `dist/` output served by nginx as before.

#### Scenario: Image build succeeds

- **WHEN** the `apps/web` Docker image is built
- **THEN** the build uses `pnpm install --frozen-lockfile` and `pnpm run build`, and the resulting image serves the built frontend via nginx

### Requirement: Documentation and scripts reference pnpm

Project scripts and documentation that show web install/build/dev commands SHALL use pnpm commands, including `scripts/start.sh`, `README.md`, `docs/engineering.md`, and `openspec/project.md`.

#### Scenario: New developer follows the README

- **WHEN** a developer follows the web setup or build instructions in `README.md` or `docs/engineering.md`
- **THEN** every shown command uses pnpm and works as written

#### Scenario: Local dev startup script

- **WHEN** a developer runs `scripts/start.sh`
- **THEN** the script checks for and uses pnpm to start the web dev server
