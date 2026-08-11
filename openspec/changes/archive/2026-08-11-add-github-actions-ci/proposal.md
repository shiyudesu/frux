## Why

Frux is now public, but pull requests and pushes are not automatically checked. Contributors can merge code that fails Go tests, strict TypeScript, the Web test suite, Compose validation, or OpenSpec validation without any repository-level signal.

## What Changes

- Add a GitHub Actions workflow for pushes to `main` and pull requests.
- Run backend tests and compile both Go entry points with the Go version declared by `go.mod`.
- Enforce Go formatting and `go vet` before backend tests and builds.
- Install the Web dependencies with the exact pnpm version declared in `package.json`, then run ESLint, Vitest, and the production/type-check build.
- Validate Docker Compose configuration with required non-secret CI environment values.
- Validate all OpenSpec artifacts in strict mode.
- Validate GitHub Actions workflow files with actionlint.
- Use least-privilege workflow permissions, concurrency cancellation, dependency caches, and explicit timeouts.

## Capabilities

### New Capabilities

- `continuous-integration`: Defines the required automated checks, triggers, reproducible toolchain setup, permissions, and failure behavior for GitHub Actions.

### Modified Capabilities

None.

## Impact

- Adds `.github/workflows/ci.yml` and Web ESLint configuration.
- Adds Web lint development dependencies and a `pnpm run lint` script.
- Adds a new OpenSpec capability under `openspec/specs/continuous-integration/`.
- Updates public documentation to describe the CI checks and badge.
- Does not change runtime APIs, production dependencies, database schemas, or deployment behavior.
