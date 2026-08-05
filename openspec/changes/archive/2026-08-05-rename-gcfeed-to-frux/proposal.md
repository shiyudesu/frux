## Why

The product currently exposes GCFeed as a user-facing brand, source-code module identity, and runtime namespace across the Web client, backend, browser state, messaging, storage, observability, deployment, and documentation. The project is still in development and its disposable data makes this the right time to establish Frux as the single canonical identity without carrying legacy aliases or migration compatibility.

## What Changes

- Establish `Frux` as the product name, `FRUX` as the display wordmark, `F` as the compact mark, `frux` as the lowercase technical slug, and `FRUX_` as the environment-variable prefix.
- Replace the current product name in all active Web copy, accessibility labels, page metadata, documentation, project context, current OpenSpec specifications, templates, dashboards, tests, and repository instructions.
- Rename the Go module and imports, Web package identity, CSS brand tokens, binaries, images, containers, files, temporary prefixes, worker identities, and other source-level identifiers to Frux-aligned names.
- **BREAKING**: Rename browser storage keys and cookies without reading or migrating legacy values; existing development sessions, preferences, cached profiles, and pending client events may be discarded.
- **BREAKING**: Rename PostgreSQL database/user defaults, RabbitMQ exchanges/queues/headers, object-storage credentials/bucket/URLs, Prometheus metric namespace, Grafana UIDs, environment variables, Docker resources, and Kubernetes namespace/resources without preserving legacy runtime aliases or data.
- Update tests, examples, startup commands, dashboards, alerts, and validation expectations so a freshly provisioned Frux stack builds and operates consistently.
- Preserve historical OpenSpec archives and Git history as historical records; rename the local checkout directory and repository slug separately when the tracked change is complete.

## Capabilities

### New Capabilities

- `product-identity`: Defines the canonical Frux brand, technical namespace, runtime identifiers, active-reference cleanup, and allowed historical exclusions.

### Modified Capabilities

- `douyin-style-web-experience`: Replaces the GCFeed wordmark, compact mark, shell copy, and authentication presentation with the canonical Frux identity while preserving the original locally owned icon system and existing behavior.
- `platform-basics`: Makes Frux the project name used by active documentation, engineering guidance, OpenSpec context, and module documentation baselines.

## Impact

- **Frontend:** page title, brand component, authentication and navigation copy, package metadata, CSS tokens, localStorage keys, cookies, tests, and fixtures.
- **Backend:** Go module/import paths, environment variables, cookies, RabbitMQ topology and headers, metric namespaces, media temporary names, worker ownership identifiers, integration-test schemas, and test expectations.
- **Infrastructure:** Docker Compose, Dockerfiles, PostgreSQL defaults, MinIO credentials and bucket, Kubernetes namespace/resources/images/secrets, Prometheus jobs/rules, Grafana provisioning, dashboard filenames/UIDs/titles, and operational commands.
- **Documentation and specifications:** README, engineering/product/architecture/UI/UX/module documentation, project instructions, templates, current OpenSpec specs, and examples.
- **Operations:** existing development volumes, queues, buckets, metrics history, browser state, and local credentials are intentionally disposable and will be recreated under the Frux namespace.
