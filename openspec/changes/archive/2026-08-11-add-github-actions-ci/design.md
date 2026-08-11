## Context

Frux has verified local commands for Go, pnpm, Docker Compose, and OpenSpec, but GitHub does not execute them for pushes or pull requests. The repository contains two Go entry points, one standalone pnpm Web package, a Compose stack with required interpolation, and repo-local OpenSpec artifacts.

CI must be useful to external contributors without requiring repository secrets or starting stateful infrastructure. Real PostgreSQL integration tests remain opt-in because the existing suite explicitly skips them when `FRUX_POSTGRES_TEST_DSN` is absent.

## Goals / Non-Goals

**Goals:**

- Run the existing backend, Web, Compose, and OpenSpec validation commands on pushes to `main` and pull requests.
- Reproduce the declared Go, Node, and pnpm toolchains without adding project dependencies.
- Keep workflow permissions read-only and avoid exposing secrets to forked pull requests.
- Make failures easy to attribute to backend, Web, or repository configuration.
- Cancel superseded runs for the same branch or pull request.

**Non-Goals:**

- Start PostgreSQL, Redis, Kafka, MinIO, Prometheus, or Grafana in CI.
- Run the optional real PostgreSQL integration suite.
- Build or publish Docker images.
- Deploy environments, upload artifacts, or use repository secrets.
- Add browser smoke tests before a deterministic CI fixture is available.

## Decisions

### Separate jobs by failure domain

Use `backend`, `web`, and `repository` jobs. Separate jobs run in parallel and make the failing area visible in the GitHub checks UI.

An alternative single job was rejected because Go and Node setup would be serialized, the logs would be harder to scan, and a frontend dependency failure would prevent independent backend feedback.

### Reuse declared toolchain versions

The backend job uses `actions/setup-go` with `apps/api/go.mod`. The Web job uses Node 22, `pnpm/action-setup`, the exact `pnpm@10.33.2` version declared in `apps/web/package.json`, and the committed lockfile.

The repository job runs OpenSpec through pinned `@fission-ai/openspec@1.6.0`, matching the current project CLI, instead of depending on a preinstalled runner command.

### Run existing commands without new test infrastructure

The backend job rejects files that are not `gofmt`-clean, runs `go vet ./...`, executes `go test ./...`, and compiles `./cmd/feed` plus `./cmd/worker`.

The Web package uses ESLint flat configuration with JavaScript recommended rules, TypeScript recommended rules, React Hooks rules, and the Vite React Refresh export rule. CI runs the repository-owned `pnpm run lint` script before Vitest and the strict production build.

The repository job supplies a non-secret CI-only internal token to `docker compose config --quiet`, runs strict OpenSpec validation, and runs pinned actionlint against workflow YAML.

The optional PostgreSQL suite is excluded because provisioning only PostgreSQL does not cover every integration dependency, and the repository already defines explicit opt-in behavior for that suite.

An external Go lint aggregator was rejected because the current codebase has no established configuration and adding a broad default rule set would create an unrelated cleanup project. `gofmt` and `go vet` provide deterministic standard-library gates. ESLint is added for the TypeScript/React source because TypeScript compilation does not catch hook misuse, unsafe control flow, or common JavaScript errors.

### Least privilege and bounded execution

The workflow grants only `contents: read`, sets per-job timeouts, and uses concurrency cancellation keyed by workflow and ref. It does not use `pull_request_target`, write permissions, deployment credentials, or cache write scripts from untrusted code.

### Public status badge

README links to the workflow badge after the workflow exists. The badge represents the combined workflow result rather than claiming deployment or release status.

## Risks / Trade-offs

- [Go tests become slower as the suite grows] → Use the built-in Go module/build cache and split jobs only when timing data justifies it.
- [pnpm or OpenSpec package retrieval is unavailable] → Pin versions and use standard setup actions; transient registry failures remain visible rather than silently skipped.
- [Initial ESLint rollout exposes existing violations] → Start from recommended correctness rules, fix current findings, and avoid speculative style-only rules.
- [actionlint requires Go setup in the repository job] → Reuse `actions/setup-go` and pin the actionlint module version.
- [Compose validation does not prove services start] → Keep it as schema/interpolation validation and leave runtime smoke tests to a future deterministic environment.
- [Fork pull requests can modify workflow commands] → Use read-only permissions and no secrets, so untrusted changes cannot obtain privileged credentials.
- [Action major tags can move] → Depend only on widely used official actions and `pnpm/action-setup`; commit-SHA pinning can be added by a later supply-chain hardening change.
