## Context

The current Prod Compose is image-only, but the uncommitted CD draft gives GitHub an SSH path to the server. The repository is public, so fork PRs must never gain server execution, deployment secrets, or a persistent runner. The server should instead pull an approved public GHCR deployment bundle on a slow schedule.

## Goals / Non-Goals

**Goals:**

- Build only the exact same-repository `main` SHA that passed CI.
- Publish API and Web images to public GHCR.
- Require owner approval through the protected `production` Environment before advancing Prod.
- Keep `.env.prod` only on the server.
- Give the server no Git checkout, compiler, GHCR credential, deployment SSH key, webhook listener, or self-hosted runner.
- Poll once per hour, exit cheaply when unchanged, preserve Worker state, verify health, and roll back the whole release bundle.

**Non-Goals:**

- Deploying PR code or failed CI runs.
- Supporting private GHCR packages.
- Automatically rolling back database migrations.
- Replacing the existing single-server runtime architecture.

## Decisions

### Separate image build from Prod promotion

`.github/workflows/deploy.yml` runs after workflow `CI` completes. It proceeds only when CI succeeded, `head_branch` is `main`, and `head_repository` is this repository.

A GitHub-hosted build job checks out the exact tested SHA and pushes:

- `ghcr.io/shiyudesu/frux-api:<sha>`
- `ghcr.io/shiyudesu/frux-web:<sha>`

A second job targets the protected `production` Environment and waits for owner approval. It creates and pushes:

- `ghcr.io/shiyudesu/frux-deploy:<sha>`
- `ghcr.io/shiyudesu/frux-deploy:prod`

Only the approved job can advance the mutable `prod` pointer.

### Pin application images by digest

The build job returns the API and Web image digests. The deployment bundle contains:

```dotenv
FRUX_API_IMAGE=ghcr.io/shiyudesu/frux-api@sha256:...
FRUX_WEB_IMAGE=ghcr.io/shiyudesu/frux-web@sha256:...
FRUX_RELEASE_SHA=<tested-sha>
```

Prod Compose consumes complete image references and does not append a tag.

### Package deployment files as an OCI image

The small Alpine `frux-deploy` image contains `/bundle` with:

```text
apps/docker-compose.prod.yml
apps/.env.prod.example
apps/.env.release
apps/api/configs/config.prod.yaml
scripts/postgres-backup.sh
manifest.sha256
```

The manifest covers every file except itself. No source code or real secret is included.

### Poll through a fixed systemd service

The server installs:

```text
/usr/local/sbin/frux-deploy
/etc/systemd/system/frux-deploy.service
/etc/systemd/system/frux-deploy.timer
/opt/frux/.env.prod
```

The timer starts two minutes after boot and then one hour after each completed check. `RandomizedDelaySec=5min` avoids a fixed synchronized request time, and `Persistent=true` catches a missed run after reboot.

The script uses a non-blocking `flock`, anonymously pulls `ghcr.io/shiyudesu/frux-deploy:prod`, compares its repository digest with the deployed digest, and exits when unchanged. A normal unchanged check only contacts GHCR and does not download image layers or recreate containers.

### Verify, deploy, and roll back locally

For a new digest, the script copies `/bundle` into a temporary release directory, rejects unexpected files or symlinks, verifies `manifest.sha256`, and atomically moves it to `/opt/frux/releases/<digest>`.

It reads the server-owned `/opt/frux/.env.prod` and bundle-owned `.env.release`, preserves whether Worker is enabled, pulls digest-pinned API/Web images, and runs Compose.

On success, `/opt/frux/current` points to the new release and the deployed bundle digest is recorded. On failure, the previous release is recreated and remains current. Only the current and previous successful release directories are retained.

### Harden the public-repository path

- Fork PR CI runs on GitHub-hosted runners with read-only permissions and no secrets.
- `pull_request_target` is not used.
- No self-hosted runner is registered to the public repository.
- The privileged workflow validates same-repository `main` and checks out the exact CI-tested SHA.
- Only the promotion job targets the `production` Environment, which must require owner approval and restrict deployments to `main`.
- CODEOWNERS identifies workflows, Dockerfiles, Prod Compose/configuration, and deployment-agent files for review. The current solo-maintainer branch rule requires PR plus CI but no impossible self-approval; the protected Prod Environment remains the separate manual release gate.
- Privileged workflow actions are pinned to commit SHAs.
- GHCR API, Web, and deploy packages are made public once, so the server stores no registry credential.

## Risks / Trade-offs

- [GHCR packages remain private] → The hourly pull fails without changing the current release; package visibility is a documented one-time setup.
- [A malicious change reaches main] → Require PR-only main, successful CI, owner diff review, and a separate Prod Environment approval; enable mandatory code-owner approval when another trusted maintainer is added.
- [Polling overlaps a prior deployment] → The script uses `flock`; the second run exits.
- [Application rollback cannot reverse schema changes] → Keep migrations backward compatible and limit automatic rollback to images/configuration.
- [The deployment agent itself needs an update] → Keep it small, root-owned, manually installed, and update it explicitly rather than from the deploy bundle.

## Migration Plan

1. Change Prod Compose to complete digest image references.
2. Replace the SSH workflow with CI-gated image build and reviewer-approved deploy-bundle promotion.
3. Add the fixed deployment script and systemd hourly timer.
4. Add CODEOWNERS and document repository/Environment settings.
5. Validate workflow trust checks, image digests, bundle allowlist/checksums, unchanged polling, successful deployment, rollback, Worker preservation, locking, and timer behavior.
6. Make GHCR packages public, install the server agent/timer, create `.env.prod`, approve one promotion, and complete a live rollback drill.

## Open Questions

None.
