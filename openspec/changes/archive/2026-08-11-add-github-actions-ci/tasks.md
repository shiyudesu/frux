## 1. Workflow foundation

- [x] 1.1 Add `.github/workflows/ci.yml` with pull request, `main` push, and manual triggers, read-only permissions, concurrency cancellation, and explicit job timeouts.
- [x] 1.2 Add the backend job using `apps/api/go.mod` for Go setup, module caching, `go test ./...`, and compilation of `cmd/feed` and `cmd/worker`.
- [x] 1.3 Add the Web job using Node 22, pnpm 10.33.2, frozen lockfile installation, Vitest, and the strict production build.
- [x] 1.4 Add the repository job for Docker Compose configuration validation and pinned OpenSpec strict validation without repository secrets.

## 2. Public documentation

- [x] 2.1 Add the CI workflow badge to `README.md` and describe the automated checks without claiming deployment status.
- [x] 2.2 Update the project documentation map or engineering verification guidance if needed to identify CI as the pull-request gate.

## 3. Verification

- [x] 3.1 Run the backend commands locally: full Go tests and both production builds.
- [x] 3.2 Run the Web frozen install, complete Vitest suite, and strict production build locally.
- [x] 3.3 Run Compose configuration validation and `openspec validate --all --strict`.
- [x] 3.4 Validate the workflow YAML structure and confirm the repository has no required CI secrets or write permissions.

## 4. Lint gates

- [x] 4.1 Add Go formatting and `go vet ./...` steps to the backend CI job.
- [x] 4.2 Add ESLint flat configuration, pinned Web development dependencies, and a `pnpm run lint` script.
- [x] 4.3 Add the Web lint step and pinned actionlint execution to GitHub Actions.
- [x] 4.4 Fix current lint findings without weakening strict TypeScript or React Hook behavior.
- [x] 4.5 Run Go formatting/vet, Web lint/tests/build, actionlint, Compose validation, and strict OpenSpec validation.
