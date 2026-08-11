# continuous-integration Specification

## Purpose

Defines the required GitHub Actions continuous integration workflow for Frux: its triggers and concurrency behavior, backend and Web verification commands, repository configuration validation, least-privilege execution, and public status reporting.

## Requirements

### Requirement: CI triggers and concurrency

Frux SHALL run continuous integration for pull requests, pushes to `main`, and explicit manual dispatch. Runs for the same workflow and Git ref SHALL cancel older in-progress runs.

#### Scenario: Pull request is updated

- **WHEN** a contributor opens or updates a pull request
- **THEN** GitHub Actions runs the complete CI workflow for the proposed revision

#### Scenario: Branch receives a newer revision

- **WHEN** a newer commit starts CI for the same pull request or branch while an older run is active
- **THEN** GitHub cancels the superseded run and keeps the newest run

### Requirement: Backend verification

CI SHALL use the Go version declared by `apps/api/go.mod`, reject Go files that are not formatted by `gofmt`, run `go vet ./...`, run the complete Go test suite, and compile both production entry points.

#### Scenario: Backend revision is valid

- **WHEN** Go formatting, `go vet ./...`, `go test ./...`, and both production builds pass
- **THEN** the backend CI job succeeds

#### Scenario: Backend formatting, vet, test, or build fails

- **WHEN** a Go file is not formatted, `go vet` reports an error, a Go test fails, or either production entry point does not compile
- **THEN** the backend CI job fails

### Requirement: Reproducible Web verification

CI SHALL use Node 22, pnpm `10.33.2`, and `apps/web/pnpm-lock.yaml` to install dependencies without modifying the lockfile, then run ESLint, the Web test suite, and the strict production build.

#### Scenario: Web revision is valid

- **WHEN** frozen dependency installation, ESLint, Vitest, TypeScript checking, and Vite production build all succeed
- **THEN** the Web CI job succeeds

#### Scenario: Lockfile, lint, test, type, or build failure

- **WHEN** the frozen install detects drift or ESLint, a Web test, TypeScript check, or production build fails
- **THEN** the Web CI job fails

### Requirement: Repository configuration verification

CI SHALL validate Docker Compose interpolation and schema with non-secret CI-only values, SHALL validate all OpenSpec artifacts in strict mode, and SHALL validate GitHub Actions workflow files with actionlint.

#### Scenario: Repository configuration is valid

- **WHEN** Compose validation, strict OpenSpec validation, and actionlint all succeed
- **THEN** the repository CI job succeeds

#### Scenario: Compose, OpenSpec, or workflow configuration is invalid

- **WHEN** Compose validation fails, an OpenSpec artifact is invalid, or actionlint rejects a workflow
- **THEN** the repository CI job fails

### Requirement: Least-privilege execution

The CI workflow SHALL use read-only repository permissions, SHALL NOT require repository secrets, and SHALL bound each job with an explicit timeout.

#### Scenario: Fork pull request runs CI

- **WHEN** CI runs for an untrusted fork pull request
- **THEN** all required checks can execute without write permissions or protected credentials

#### Scenario: Job hangs

- **WHEN** a CI command exceeds its job timeout
- **THEN** GitHub terminates the job and reports failure

### Requirement: Public workflow status

The public README SHALL display a badge linked to the GitHub Actions CI workflow.

#### Scenario: Reader checks repository status

- **WHEN** a reader opens the README
- **THEN** the CI badge shows the current workflow state and links to workflow runs
