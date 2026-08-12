## Why

The Prod server should run released container images without cloning source code, compiling Go/Node, accepting deployment SSH keys, or exposing a webhook. GitHub Actions can publish immutable images and an approved deployment-bundle image to public GHCR, while the server checks for a new approved bundle once per hour.

## What Changes

- Publish API/Worker and Web images to GHCR with commit-SHA and `latest` tags after CI succeeds on `main`.
- Change Prod Compose to pull configurable GHCR images instead of using local build contexts.
- Package Compose, application configuration, backup script, checksums, and digest-pinned image references into `ghcr.io/shiyudesu/frux-deploy`.
- Promote the mutable `frux-deploy:prod` pointer only after the protected GitHub `production` Environment is approved.
- Add a fixed server deployment script and systemd timer that anonymously poll public GHCR once per hour.
- Preserve the server-owned `.env.prod`, Worker state, persistent volumes, current release, and previous release.
- Verify checksums and service health, and roll back the whole deployment bundle if the new release fails.
- Add CODEOWNERS guidance and public-repository safeguards so fork PRs cannot publish or deploy.

## Capabilities

### New Capabilities

- `ghcr-prod-deployment`: Defines CI-gated image publishing and source-free pull-based Prod delivery through public GHCR, protected promotion, hourly systemd polling, health checks, and rollback.

### Modified Capabilities

None.

## Impact

- Adds a GitHub Actions deployment workflow and GHCR package permissions.
- Changes `docker-compose.prod.yml` image configuration and removes Prod build contexts.
- Adds a root-owned deployment script, systemd service, and hourly timer.
- Updates Prod deployment documentation, GitHub Environment settings, branch protection, and CODEOWNERS.
- The server retains Docker, `.env.prod`, the deployment script/timer, two release directories, volumes, and images, but no Git repository, build toolchain, deploy SSH key, or webhook service.
