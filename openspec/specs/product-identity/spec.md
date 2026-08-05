# product-identity Specification

## Purpose

Defines the canonical Frux product brand, source and artifact identity, runtime namespace, clean development cutover, and active-reference completeness rules.

## Requirements

### Requirement: Canonical Frux identity
The product SHALL use `Frux` as its canonical product name, `FRUX` as its display wordmark, `F` as its compact brand mark, `frux` as its lowercase technical slug, and `FRUX_` as its environment-variable prefix.

#### Scenario: Product identity is rendered
- **WHEN** a user opens a branded Web, documentation, dashboard, or authentication surface
- **THEN** the surface identifies the product as Frux and uses the FRUX wordmark or F compact mark where a visual brand mark is required

#### Scenario: Technical identifiers are authored
- **WHEN** a developer adds a Frux-owned package, artifact, environment variable, runtime resource, or persisted client key
- **THEN** the identifier derives from `frux` or `FRUX_` rather than introducing another product-name variant

### Requirement: Active source and artifact identity
Active source files and build artifacts SHALL use Frux-aligned names, including the canonical `github.com/shiyudesu/frux` Go module path, `frux-web` Web package name, `frux-api` and `frux-worker` binaries and images, and `--frux-*` brand CSS tokens.

#### Scenario: Backend builds with the canonical module
- **WHEN** the API and worker are compiled from the renamed repository
- **THEN** all internal imports resolve through `github.com/shiyudesu/frux` and both Frux binaries build without legacy module imports

#### Scenario: Frontend builds with Frux identity
- **WHEN** the Web production build runs
- **THEN** package metadata, brand components, page metadata, styles, tests, and generated assets use the Frux identity without legacy brand tokens

### Requirement: Frux runtime namespace
Fresh development and deployment configuration SHALL use the Frux runtime namespace across browser storage, cookies, environment variables, PostgreSQL defaults, RabbitMQ topology and headers, object storage, Prometheus metrics, Grafana resources, Docker resources, and Kubernetes resources.

#### Scenario: Browser state uses Frux keys
- **WHEN** the Web client stores authentication, public profiles, player preferences, upload state, or pending view events
- **THEN** localStorage and Cookie names use `frux` prefixes and no legacy key is read or written

#### Scenario: Backend dependencies use Frux names
- **WHEN** a freshly configured API or worker connects to PostgreSQL, RabbitMQ, and object storage
- **THEN** it uses the `frux` database and user defaults, `frux.*` messaging resources, `x-frux-*` message headers, and the `frux-media` bucket

#### Scenario: Observability uses Frux names
- **WHEN** the API, worker, Prometheus, and Grafana start
- **THEN** emitted metrics use the `frux_` namespace and Frux job names, alerts, dashboard titles, tags, filenames, and UIDs resolve consistently

#### Scenario: Deployment uses Frux resources
- **WHEN** Docker Compose or Kubernetes provisions a fresh stack
- **THEN** Frux container, image, namespace, Secret, command, and resource names are used and the stack reaches its existing health checks

### Requirement: Clean development cutover
The rename SHALL perform a clean cutover without compatibility aliases, dual reads, dual writes, data copying, metric bridging, queue draining, or automatic migration of development state.

#### Scenario: Legacy development state exists
- **WHEN** a developer upgrades from a checkout that used the previous product namespace
- **THEN** they recreate disposable containers, volumes, queues, buckets, browser state, credentials, and metrics under the Frux namespace

#### Scenario: Legacy runtime identifier is supplied
- **WHEN** only a legacy environment variable, browser key, queue, bucket, database, or deployment resource exists
- **THEN** the renamed system does not treat it as an alias for the corresponding Frux identifier

### Requirement: Active-reference completeness
After the change is implemented and synchronized, active product source, configuration, documentation, tests, templates, filenames, and baseline specifications SHALL contain no legacy product identifiers or previous brand-derived CSS tokens.

#### Scenario: Repository identity audit runs
- **WHEN** tracked active files and filenames are searched case-insensitively for legacy product names, prefixes, and brand tokens
- **THEN** no match remains outside explicitly historical change artifacts

#### Scenario: Historical records are inspected
- **WHEN** Git history or archived OpenSpec changes describe behavior created under the previous name
- **THEN** those historical records may retain the name that was accurate when they were authored
