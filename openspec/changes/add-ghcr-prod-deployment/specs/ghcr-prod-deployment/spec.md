## ADDED Requirements

### Requirement: CI-Gated Image Publication
API and Web images SHALL be published only after CI succeeds for the exact same-repository `main` SHA.

#### Scenario: Fork PR runs CI
- **WHEN** untrusted fork code triggers a pull-request workflow
- **THEN** it receives no Prod secret, package-write permission, self-hosted runner, or deployment path

#### Scenario: Main CI fails
- **WHEN** any required CI job fails
- **THEN** no image or deployable bundle is published for that SHA

### Requirement: Reviewer-Protected Prod Promotion
The mutable `frux-deploy:prod` pointer SHALL advance only from a job protected by the GitHub `production` Environment and approved by an authorized reviewer.

#### Scenario: Images are built but deployment is not approved
- **WHEN** API and Web images exist for a successful SHA but the Environment job is not approved
- **THEN** the server-visible `prod` deployment pointer remains unchanged

### Requirement: Digest-Pinned Release
Each approved deployment bundle SHALL reference API and Web images by immutable GHCR digest.

#### Scenario: Release tag is overwritten
- **WHEN** a convenience image tag later points to another image
- **THEN** an existing release still pulls the original digest-pinned image

### Requirement: Source-Free Public Bundle
The public GHCR deployment image SHALL contain only allowlisted deployment files, a checksum manifest, and no source code or real Prod secret.

#### Scenario: Server extracts a bundle
- **WHEN** a new `frux-deploy:prod` digest is detected
- **THEN** the server rejects unexpected paths, symlinks, checksum failures, or a bundled `.env.prod`

### Requirement: Server-Owned Secrets
The deployment agent SHALL read `/opt/frux/.env.prod` and SHALL never create, download, replace, or upload it.

#### Scenario: Prod environment file is missing
- **WHEN** a new approved bundle is detected without `/opt/frux/.env.prod`
- **THEN** deployment stops before pulling application images or recreating containers

### Requirement: Hourly Pull Agent
The server SHALL check public GHCR through a locked systemd oneshot service one hour after each completed check, with boot recovery and randomized delay.

#### Scenario: Approved bundle is unchanged
- **WHEN** the timer runs and the `prod` repository digest equals the deployed digest
- **THEN** the service exits without downloading image layers or recreating containers

#### Scenario: Server was powered off at a scheduled time
- **WHEN** the timer becomes active after reboot
- **THEN** `Persistent=true` causes a missed check to run

### Requirement: Controlled Compose Update
The deployment agent SHALL preserve persistent volumes and update Worker only if Worker was already enabled.

#### Scenario: Worker is disabled
- **WHEN** a new bundle is deployed while no Worker container exists
- **THEN** Worker remains disabled

#### Scenario: Worker is enabled
- **WHEN** a new bundle is deployed while Worker exists
- **THEN** Worker is recreated with the approved API image digest

### Requirement: Health-Gated Bundle Rollback
The deployment agent SHALL switch `current` only after API and Web are healthy and SHALL restore the previous bundle if deployment fails.

#### Scenario: New API is unhealthy
- **WHEN** API or Web fails to become healthy within the timeout
- **THEN** the previous Compose/configuration and digest-pinned images are recreated without deleting volumes

### Requirement: Public Repository Protection
Privileged files and deployment SHALL be protected by CODEOWNERS, branch protection, GitHub-hosted runners, minimal permissions, pinned actions, and reviewer-protected Environment promotion.

#### Scenario: PR changes a deployment workflow
- **WHEN** a contributor modifies a privileged workflow or deployment file
- **THEN** the repository rules require the configured code owner to approve before merge
