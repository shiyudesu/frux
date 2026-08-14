## 1. Digest-Pinned Prod Compose

- [x] 1.1 Replace Prod API/Worker/Web build contexts and tag composition with complete GHCR image references.
- [x] 1.2 Add a non-secret release environment contract containing API/Web digests and the tested SHA.

## 2. Protected GHCR Publication

- [x] 2.1 Replace the SSH draft with a workflow triggered only by successful same-repository `main` CI completion.
- [x] 2.2 Add a read-only path-detection job comparing the previous successful `main` CI SHA with the current tested SHA.
- [x] 2.3 Gate image build and Prod promotion on runtime/Prod deployment paths while skipping documentation and template-only updates.
- [x] 2.4 Build and push API and Web images with SHA tags and capture their immutable digests.
- [x] 2.5 Add a reviewer-protected `production` Environment job that builds and promotes public `frux-deploy:<sha>` and `frux-deploy:prod` images.
- [x] 2.6 Include only allowlisted deployment files, digest release references, and `manifest.sha256` in the deploy image.
- [x] 2.7 Pin privileged workflow actions to commit SHAs and use minimal job permissions.

## 3. Hourly Server Pull Agent

- [x] 3.1 Add a root-owned deployment script that uses `flock`, anonymously polls `frux-deploy:prod`, and exits when unchanged.
- [x] 3.2 Validate bundle paths, reject symlinks/secrets, verify checksums, and install a versioned release directory.
- [x] 3.3 Preserve Worker state, deploy digest-pinned images, health-check API/Web, roll back the previous bundle, and prune old releases.
- [x] 3.4 Add a systemd oneshot service and persistent timer using a two-minute boot delay, one-hour interval, and five-minute randomized delay.

## 4. Public Repository Hardening and Documentation

- [x] 4.1 Add CODEOWNERS for workflows, Dockerfiles, Prod Compose/configuration, and deployment-agent files.
- [x] 4.2 Document branch protection, Environment approval, action policy, public GHCR packages, and prohibition on `pull_request_target` and public-repository self-hosted runners.
- [x] 4.3 Document server bootstrap without cloning, `.env.prod`, hourly polling, immediate manual check, status/log commands, and rollback.

## 5. Verification

- [x] 5.1 Validate workflow syntax, same-repository/main trust checks, pinned actions, and minimal permissions.
- [x] 5.2 Test relevant-code, deployment-config, documentation-only, multi-commit, previous-successful-CI, and missing-baseline path detection.
- [x] 5.3 Validate Prod Compose resolves complete digest references and has no build contexts.
- [x] 5.4 Verify the deploy image contains only allowlisted files, valid checksums, and no secrets/source.
- [x] 5.5 Test unchanged polling, Worker preservation, successful update, failed-health rollback, release pruning, and lock behavior with local fixture images.
- [x] 5.6 Validate systemd units, hourly schedule, boot recovery, and manual service start.
- [x] 5.7 Run repository tests/build, Compose validation, and `openspec validate --all --strict`.
- [x] 5.8 Make real GHCR packages public, configure protected Prod promotion, and complete a live deployment and rollback drill.
