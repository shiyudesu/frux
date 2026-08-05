## 1. Establish the Rename Baseline

- [x] 1.1 Inventory tracked active content and filenames containing `GCFeed`, `gcfeed`, `GCFEED_`, `--gc-*`, and brand-derived filenames, recording historical OpenSpec exclusions separately.
- [x] 1.2 Apply the canonical identity map consistently: Frux product name, FRUX wordmark, F compact mark, `frux` slug, `FRUX_` environment prefix, and `github.com/shiyudesu/frux` module path.
- [x] 1.3 Confirm that API routes, business behavior, database tables, and identifiers unrelated to the product name remain unchanged.

## 2. Rename the Web Product and Client Identity

- [x] 2.1 Replace page metadata, BrandMark text and accessibility labels, the compact G mark, login copy, navigation copy, comments, and frontend test expectations with Frux, FRUX, and F.
- [x] 2.2 Rename the Web package from `gcfeed-web` to `frux-web` and replace brand-derived test hosts, fixtures, and source comments.
- [x] 2.3 Rename all `--gc-*` CSS custom properties and references to `--frux-*` without changing their visual values or responsive behavior.
- [x] 2.4 Rename authentication, public-profile, player-preference, and pending-view-event localStorage keys to `frux.*` forms, with no fallback reads or migration of legacy values.
- [x] 2.5 Rename frontend asset-active Cookie usage to `frux_asset_active` and update related logout, upload, session, and test behavior.
- [x] 2.6 Run the focused frontend tests covering BrandMark/navigation, session persistence, upload identity, player preferences, and pending view-event delivery.

## 3. Rename Go Source and Build Artifacts

- [x] 3.1 Change `apps/api/go.mod` to `module github.com/shiyudesu/frux` and update every internal Go import and documentation example to the canonical module path.
- [x] 3.2 Rename API and worker build outputs, Dockerfile copy targets, entrypoint, Compose command, Kubernetes command, and related tests to `frux-api` and `frux-worker`.
- [x] 3.3 Replace backend brand-derived temporary-file prefixes, worker/cleanup ownership strings, integration-test schema prefixes, fake hostnames, comments, and assertions with Frux-aligned values.
- [x] 3.4 Compile both Go entry points after the module and artifact rename before proceeding to runtime namespace changes.

## 4. Rename Stateful Runtime Configuration

- [x] 4.1 Rename `GCFEED_INTERNAL_TOKEN` and `GCFEED_POSTGRES_TEST_DSN` to `FRUX_INTERNAL_TOKEN` and `FRUX_POSTGRES_TEST_DSN` in configuration loading, tests, Compose, manifests, OpenSpec context, and documentation.
- [x] 4.2 Rename backend asset Cookies to `frux_asset_token` and `frux_asset_active`, keeping frontend and HTTP middleware constants synchronized without compatibility aliases.
- [x] 4.3 Rename PostgreSQL development database/user defaults, DSNs, health checks, test schemas, Compose values, Kubernetes values, and examples to `frux`.
- [x] 4.4 Rename every RabbitMQ exchange, queue, consumer default, configuration value, test expectation, and `x-gcfeed-*` message header to the `frux.*` and `x-frux-*` namespaces.
- [x] 4.5 Rename MinIO development credentials, `gcfeed-media` bucket and URLs, storage configuration, tests, and documentation to Frux equivalents.
- [x] 4.6 Verify API and worker configuration tests reject missing Frux variables and do not accept legacy variable names as aliases.

## 5. Rename Observability Resources

- [x] 5.1 Change every backend Prometheus namespace from `gcfeed` to `frux` and update metric-family tests and runtime HTTP assertions.
- [x] 5.2 Update Prometheus scrape job names, alert group names, alert expressions, service labels, and monitoring documentation to consume `frux_*` metrics.
- [x] 5.3 Rename Grafana dashboard titles, tags, UIDs, JSON filenames, provisioning paths, README links, and documented dashboard paths to Frux.
- [x] 5.4 Verify every dashboard query and alert expression references an emitted Frux metric and no monitoring file references a legacy metric family.

## 6. Rename Deployment and Provisioning Identity

- [x] 6.1 Rename all Docker Compose container names, development credentials, bucket initialization commands, environment references, and Frux-owned commands while preserving service-to-service hostnames that are intentionally generic.
- [x] 6.2 Rename the Kubernetes namespace, Secret, image references, commands, selectors or labels that contain the product identity, and every namespace-qualified resource reference to Frux.
- [x] 6.3 Rename Frux-owned monitoring provisioning filenames and any other tracked filenames containing the legacy product identifier, updating all references to the moved files.
- [x] 6.4 Update startup, cleanup, test, deployment, and monitoring commands so they provision and address only the Frux stack.

## 7. Rename Active Documentation and Specifications

- [x] 7.1 Replace the legacy product identity throughout README, product, architecture, engineering, quick-read, UI/UX, optimization, performance, security, deployment, resume, interview, current-problem, and module documentation while preserving technical meaning.
- [x] 7.2 Update `.github` repository instructions, templates, comments, examples, diagrams, dashboard descriptions, and accessibility guidance to Frux.
- [x] 7.3 Update `openspec/project.md`; synchronize this change's delta specs into the baseline specs and replace remaining editorial legacy references in active `openspec/specs/**` without editing archived changes.
- [x] 7.4 Check documentation links, dashboard URLs, commands, module import examples, environment-variable examples, and filenames after all moves.

## 8. Validate the Complete Cutover

- [x] 8.1 Run all Go tests and compile `./cmd/feed` and `./cmd/worker` using the canonical Frux module and binary identities.
- [x] 8.2 Run all Web tests and the production TypeScript/Vite build with the renamed package, brand, storage keys, cookies, and CSS tokens.
- [x] 8.3 Validate Docker Compose, Kubernetes manifests, and all OpenSpec artifacts with the repository's existing validation commands.
- [x] 8.4 Audit tracked active file contents and filenames for `GCFeed`, `gcfeed`, `GCFEED`, and `--gc-`, allowing matches only in Git history and explicitly historical OpenSpec change artifacts.
- [x] 8.5 Remove disposable legacy development containers, volumes, browser state, queues, buckets, metrics, and Kubernetes resources, then provision a fresh Frux stack and verify Web, API health, worker metrics, media storage, RabbitMQ, Prometheus, and Grafana connectivity.
- [x] 8.6 Rename the GitHub repository slug to `frux`, update the Git remote, rename the local checkout directory to `Frux` or `frux`, and repeat module-resolution and startup smoke checks from the new path.
