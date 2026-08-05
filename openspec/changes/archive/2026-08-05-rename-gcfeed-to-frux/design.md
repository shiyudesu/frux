## Context

GCFeed currently acts as three identities at once: the visible product brand, the source/build namespace, and the runtime/deployment namespace. The name appears in active Web surfaces and documents, the `module GCFeed` declaration and hundreds of Go imports, CSS tokens, browser keys and cookies, RabbitMQ resources, PostgreSQL and MinIO defaults, Prometheus metrics, Grafana dashboards, Docker artifacts, Kubernetes resources, tests, temporary names, and operational examples.

The project is still in development. Existing browser state, databases, queues, buckets, volumes, and metrics history are disposable, so the rename does not need compatibility aliases or data migration. Archived OpenSpec changes and Git history remain immutable historical evidence and are not part of the active-reference cleanup.

## Goals / Non-Goals

**Goals:**

- Make Frux the only identity used by active product surfaces, source code, configuration, runtime resources, deployment artifacts, documentation, tests, and baseline specifications.
- Use one canonical mapping for capitalization, slugs, prefixes, marks, packages, binaries, and infrastructure resources.
- Keep API behavior, business rules, routes, data models, and user workflows unchanged.
- Leave a freshly provisioned Compose and Kubernetes stack internally consistent and verifiable.
- Provide a deterministic audit proving no active legacy product identifier remains.

**Non-Goals:**

- Preserving existing development sessions, browser preferences, pending events, databases, queues, object-storage data, metrics history, dashboard URLs, or Kubernetes resources.
- Supporting old and new environment variables, cookies, storage keys, message resources, metric names, or deployment names simultaneously.
- Rewriting Git history or archived OpenSpec changes.
- Redesigning the product beyond replacing the wordmark and compact brand letter.
- Changing API resource paths or database table names that do not contain the product identity.

## Decisions

### 1. Use one change with ordered cutover phases

The complete rename will remain one OpenSpec change so the canonical identity and final completeness check cannot drift across independent proposals. Implementation tasks will still be grouped into reviewable phases:

1. product surfaces and active documentation;
2. source, package, build, and file identity;
3. browser, backend, observability, and deployment runtime namespaces;
4. clean reprovisioning, validation, and external repository/directory rename.

Splitting these concerns into independent changes was considered, but partial completion would leave ambiguous mixed identities and make the final zero-reference contract harder to enforce.

### 2. Adopt a fixed canonical identity map

| Surface | Canonical value |
| --- | --- |
| Product name | `Frux` |
| Display wordmark | `FRUX` |
| Compact mark | `F` |
| Lowercase slug | `frux` |
| Environment prefix | `FRUX_` |
| Go module | `github.com/shiyudesu/frux` |
| Web package | `frux-web` |
| API and worker binaries/images | `frux-api`, `frux-worker` |
| CSS brand tokens | `--frux-*` |
| Browser storage | `frux.*` and `frux.*.v1` |
| Asset cookies | `frux_asset_token`, `frux_asset_active` |
| PostgreSQL defaults | database and user `frux` |
| RabbitMQ namespace | exchanges and queues under `frux.*`; headers under `x-frux-*` |
| Object storage | bucket `frux-media`; development identity `frux` |
| Prometheus | metric namespace `frux`; jobs `frux-api` and `frux-worker` |
| Grafana | Frux titles/tags, `frux-*` UIDs and filenames |
| Docker and Kubernetes | `frux-*` artifacts and Kubernetes namespace `frux` |

The Go module will use the repository-backed path rather than `module Frux`, because a canonical import path remains stable for tooling and external consumers. The GitHub repository should be renamed to `frux` before publishing builds that expect external module resolution.

### 3. Perform direct replacement without compatibility code

All runtime consumers and producers will switch together. The Web will only use Frux browser keys and cookies. The API and worker will only use Frux environment variables, messaging resources, headers, database defaults, metrics, and object-storage configuration. Configuration defaults, Docker Compose, Kubernetes manifests, tests, dashboards, alerts, and documentation will change in the same implementation.

Dual reads, aliases, queue draining, object copying, recording rules, and metric dual-emission were rejected because they would add temporary complexity for data the user has explicitly declared disposable.

### 4. Recreate stateful development resources

Existing Compose volumes and containers will be removed and recreated after configuration changes. Browser storage and cookies will be cleared. RabbitMQ, PostgreSQL, MinIO, Prometheus, Grafana, and Kubernetes resources will start fresh under Frux names.

The implementation will not add migration scripts. Rollback means reverting the rename commit and recreating a fresh stack under the previous configuration, not restoring renamed development data.

### 5. Treat active specifications and historical artifacts differently

Current project context, baseline specifications, documentation, templates, instructions, examples, and tests will use Frux. Delta specs define the normative identity changes, and the baseline specs must be synchronized when implementation is complete.

Archived OpenSpec changes and Git history will retain historically accurate wording. The final audit will therefore exclude `openspec/changes/archive/**` and the rename change artifact itself while it remains unarchived.

### 6. Validate both content and filenames

The final identity audit will search tracked active content case-insensitively for the previous product name and prefix variants, search for `--gc-*`, and inspect tracked filenames. This complements normal builds because compilation cannot detect stale documentation, dashboard UIDs, accessibility labels, temporary prefixes, or deployment names.

Validation will also compile both Go entry points, run Go tests, run Web tests and the production build, validate Compose, validate Kubernetes manifests, validate all OpenSpec artifacts, and start a freshly provisioned stack when the required local dependencies are available.

### 7. Finish external identity changes last

After tracked files are consistent, the GitHub repository slug should become `frux`, the remote URL should be updated, and the local checkout directory should be renamed from `GCFeed` to `Frux` or `frux`. These operations are external to tracked repository contents and must not be confused with application-code edits.

## Risks / Trade-offs

- **[Partial replacement breaks imports or runtime wiring]** → Apply the canonical map by phase, use repository-wide searches after every phase, and run targeted builds before proceeding.
- **[Fresh Frux services cannot access legacy data]** → This is intentional; remove and recreate disposable development state instead of adding migration logic.
- **[Metric and dashboard history disappears]** → Accept the reset during development and update emitters, queries, alerts, UIDs, filenames, and documentation atomically.
- **[RabbitMQ producers and consumers use different topology names]** → Change configuration, defaults, publishers, consumers, tests, and manifests in the same phase, then provision a fresh broker.
- **[Cookie and localStorage changes appear as logout or preference loss]** → Document the clean cutover and clear browser state; do not add fallback reads.
- **[Go module path is published before the repository slug exists]** → Rename the GitHub repository and update the remote before relying on external module resolution.
- **[Historical searches still find the previous name]** → Keep explicit audit exclusions limited to Git history and OpenSpec change archives rather than weakening the active-source requirement.

## Migration Plan

1. Record the canonical identity map and inventory active content and filenames.
2. Replace user-visible Web identity, brand marks, metadata, current documentation, project context, templates, and dashboard display text.
3. Rename the Go module/imports, Web package, CSS tokens, binaries, images, commands, temporary prefixes, worker identifiers, filenames, and tests.
4. Rename browser keys/cookies and all backend, storage, messaging, observability, Docker, and Kubernetes runtime identifiers.
5. Remove disposable legacy development resources and provision a fresh Frux stack.
6. Run targeted tests after each phase, then the full repository identity audit and project validation commands.
7. Synchronize the delta specifications into the baseline specifications and archive the completed change.
8. Rename the GitHub repository slug and local checkout directory, update the Git remote, and repeat the external-path smoke check.

## Open Questions

None. The canonical Frux values, clean-cutover policy, historical exclusions, and external repository target are fixed by this change.
