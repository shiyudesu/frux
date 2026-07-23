# GCFeed Copilot Instructions

GCFeed is a short-video feed system with a Go API/worker backend and a React/Vite frontend. Read `docs/engineering.md` before changing architecture or adding a module; use the relevant `docs/modules/*.md` file for business rules.

## Commands

Run commands from the repository root unless a command changes directory explicitly.

```bash
# Build and start the complete stack (API, worker, web, MySQL, Redis,
# RabbitMQ, Prometheus, and Grafana).
cd apps && docker compose up --build

# Validate Compose without starting services.
cd apps && docker compose config

# Start the API, worker, and Vite dev server against dependencies already
# available at the addresses in apps/api/configs/config.yaml.
./scripts/start.sh

# Compile both Go entry points.
cd apps/api && go build ./cmd/feed ./cmd/worker

# Run all Go tests.
cd apps/api && go test ./...

# Run one API-flow test. Tests in apps/api/test use package path ./test.
cd apps/api && go test ./test -run '^TestFeedAPIFlow$'

# Run one package-local unit test.
cd apps/api && go test ./internal/infra/cache -run '^TestActionStatAggregatesCounterShards$'

# Reproducible frontend install, development server, and production/type-check build.
pnpm -C apps/web install --frozen-lockfile
pnpm -C apps/web run dev
pnpm -C apps/web run build

# Inspect and validate OpenSpec artifacts.
openspec list
openspec validate --all --strict
```

Both Go binaries load `./configs/config.yaml` using a relative path, so direct `go run` commands must be executed from `apps/api`. The local startup script starts processes only; it does not provision MySQL, Redis, or RabbitMQ.

## Architecture

- `apps/api/cmd/feed` is the HTTP process. `internal/interfaces/http/router/router.go` is the main composition root: it opens GORM over the shared `database/sql` pool, runs migrations, builds repositories/services/handlers, registers middleware, and exposes `/api`, `/internal`, `/uploads`, `/health`, and `/metrics`.
- `apps/api/cmd/worker` is the asynchronous process. It consumes RabbitMQ events for interaction write-behind, following-feed fanout, feed preheating, and video embedding generation; it exposes worker metrics on `:9091`.
- Backend modules are mirrored across four layers:
  - `internal/domain/{module}`: entities, invariants, domain errors, repository interfaces.
  - `internal/application/{module}`: use cases, cursor/idempotency logic, small interfaces for optional infrastructure, and workers.
  - `internal/infra`: GORM persistence, Redis cache/indexes, RabbitMQ, JWT, configuration, metrics, and migrations.
  - `internal/interfaces/http/{module}`: DTOs, request parsing, error-to-status mapping, and handlers.
- The dependency direction is `Config/external clients -> Repository -> Application Service -> Handler -> Router`. Domain packages must not depend on Gin, GORM, Redis, or RabbitMQ types.
- MySQL is the durable source of truth. Redis holds feed pages/cards/stats, hot-ranking buckets, following indexes, and fast interaction state. RabbitMQ carries action changes, video-published events, and exposure events to workers.
- Redis and RabbitMQ are optional enhancements in the HTTP process: the router enables capabilities through application options when clients are available. The worker requires both Redis and RabbitMQ.
- `migration.AutoMigrate` is called by both processes and includes retry handling for concurrent migration races. Add new GORM models there and keep module-specific post-migration/index setup explicit.
- `apps/web` is a strict TypeScript SPA. `App.tsx` only composes providers and dispatches routes. It uses a typed, hand-written History API router (`router.tsx`), session/unread contexts (`session.tsx`), typed domain API modules over `apiRequest<T>`, page components, shared components, and behavior hooks.
- Vite proxies `/api` and `/uploads` to the local API. The production nginx image serves the SPA with history fallback and proxies the same paths to the Compose `api` service.

## Repository Conventions

- Go package names include their layer and module (`domainvideo`, `applicationvideo`, `infravideo`, `interfaceshttpvideo`); import aliases use the same names.
- A new backend module normally adds domain `entity/errors/repository`, application `service`, infrastructure `model/gorm`, HTTP `dto/handler`, router wiring, migration registration, an API-flow test, and `docs/modules/{module}.md`.
- Domain constructors clean and validate input and establish invariants. Use `Restore*` functions when reconstructing persisted state without reapplying creation defaults.
- Application services depend on domain repositories and narrow capability interfaces. Optional cache, publisher, clock, message, or strategy dependencies use functional options (`With...`) rather than concrete infrastructure imports.
- Keep handlers limited to parsing path/query/body/header values, reading auth context, invoking services, converting DTOs, and mapping domain errors with `errors.Is`.
- List APIs use stable cursor pagination based on the actual sort tuple, such as `(published_at, id)` or `(created_at, id)`, and return `items`, `next_cursor`, and `has_more`.
- Write endpoints that can be retried use `Idempotency-Key`; preserve the existing conflict behavior and 128-character limit.
- GORM models stay under `internal/infra/persistence/{module}`. Repositories return domain entities and keep transactions, stable ordering, and batch reads inside infrastructure.
- Cross-module notifications and feed backfills are connected through small application interfaces/adapters in the composition root instead of importing another module's infrastructure.
- API-flow tests under `apps/api/test` assemble Gin handlers/services with in-memory repositories and fakes, avoiding external services. Package-specific unit tests live beside the implementation.
- Frontend routes remain in the `Route` union and `normalizeRoute`; do not add a routing library. Navigation and session state flow through `useNavigate` and `useSession`, not prop drilling.
- Keep frontend API calls in `src/api/client.ts` and per-domain API modules, with request/response types in `src/types.ts`. Validate `localStorage` JSON through the existing type guards.
- Frontend TypeScript is strict: do not introduce JavaScript source, explicit `any`, `@ts-nocheck`, or `@ts-expect-error`. Preserve the current loading/error/empty/ready state handling in pages and hooks.
- pnpm is the only web package manager. Keep `apps/web/pnpm-lock.yaml` as the sole lockfile and use the exact package manager version declared in `package.json`.
- New capabilities should begin as an OpenSpec change. Keep OpenSpec specs/tasks and the affected product, architecture, engineering, module, UI/UX, or optimization docs synchronized with behavior changes.
