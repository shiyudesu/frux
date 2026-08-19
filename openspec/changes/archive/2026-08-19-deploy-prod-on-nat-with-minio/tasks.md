## 1. Reconcile Media Delivery Baseline

- [x] 1.1 Complete or explicitly resolve the remaining staged validation in `reduce-media-object-storage-egress`
- [x] 1.2 Sync or archive the media-egress change before finalizing this change so v3 exposure and 30-minute cache requirements are the implementation baseline
- [x] 1.3 Re-read the combined production-media-delivery requirements and remove any Rainyun wording that would reintroduce physical public copies or short no-store redirects

## 2. Add Production MinIO Services

- [x] 2.1 Add a pinned MinIO server service with persistent `minio_data`, backend networking, restart policy, and a bounded health check to `apps/docker-compose.prod.yml`
- [x] 2.2 Bind MinIO API and Console only to configurable loopback ports and keep the Console absent from public Caddy routes
- [x] 2.3 Add required MinIO root credential variables that are distinct from Frux S3 application credentials
- [x] 2.4 Add an idempotent `minio-init` service that creates or reuses the configured private bucket without anonymous access
- [x] 2.5 Make `minio-init` create or update a Bucket-scoped application identity and policy for the configured Frux S3 credentials
- [x] 2.6 Configure the exact Frux public application origin, including its high port, for the signed-upload methods and headers required by MinIO CORS
- [x] 2.7 Make API and Worker depend on successful MinIO initialization so storage failure prevents release readiness
- [x] 2.8 Preserve the existing development Compose MinIO behavior and credentials without importing Prod secrets

## 3. Generalize Production Storage Configuration

- [x] 3.1 Add `FRUX_S3_DOMAIN` and `FRUX_PUBLIC_HTTPS_PORT` to `apps/.env.prod.example` with no usable secret values
- [x] 3.2 Change the Prod runtime S3 endpoint to `http://minio:9000` and the browser presign endpoint to the dedicated public S3 HTTPS origin
- [x] 3.3 Include the configured public high port in the application public media base URL while keeping `FRUX_DOMAIN` a bare hostname
- [x] 3.4 Keep path-style addressing, a non-empty region, private-bucket behavior, and disabled automatic application-side bucket creation
- [x] 3.5 Add configuration validation for valid HTTPS public origins, valid port range, distinct application and S3 hostnames, and non-empty separated MinIO credentials
- [x] 3.6 Update config tests to prove internal runtime requests stay on the Compose network and presigned requests use the public high-port S3 origin
- [x] 3.7 Confirm the current Worker and FFmpeg processing profile remains enabled and unchanged

## 4. Update Release and Compose Validation

- [x] 4.1 Update CI Prod Compose environment fixtures with public-port, S3-domain, MinIO-root, and MinIO-application variables
- [x] 4.2 Assert Prod Compose contains required `minio` and `minio-init` services and that neither is hidden behind an optional profile
- [x] 4.3 Assert PostgreSQL, Redis, Kafka, Worker, and MinIO Console have no public host bindings and MinIO API binds only to loopback
- [x] 4.4 Verify the deployment bundle still contains only allowlisted configuration files and no secret or certificate material
- [x] 4.5 Verify release rollback and `docker compose down` procedures preserve `minio_data` unless the operator explicitly deletes volumes
- [x] 4.6 Validate that MinIO or initializer failure prevents API/Worker health and therefore blocks `current` release promotion

## 5. Define NAT, DNS, TLS, and Caddy Operations

- [x] 5.1 Document the required NAT mapping from one allocated public HTTPS high port to host-local 443 and from one allocated public SSH high port to host-local 22
- [x] 5.2 Document two DNS records, one for the Frux application and one for the MinIO S3 API, pointing to the NAT public address
- [x] 5.3 Document DNS-01 certificate issuance and renewal for both hostnames with least-privilege DNS API credentials stored outside the repository
- [x] 5.4 Add a Caddy configuration that serves both hostnames on local 443 with the DNS-01 certificate
- [x] 5.5 Route application API, upload, media, and health paths to the loopback API port and all other application requests to the loopback Web port
- [x] 5.6 Route the S3 hostname to the loopback MinIO API while preserving Host, path, query, method, and Range headers required by AWS Signature V4
- [x] 5.7 Document SSH-tunneled MinIO Console access and explicitly prohibit a public Console route

## 6. Update Deployment and Security Documentation

- [x] 6.1 Update `docs/deployment.md` with the NAT-host topology, private MinIO flow, high-port public origins, and single-host limitations
- [x] 6.2 Update `docs/operations/prod.md` with fresh-host bootstrap, environment variables, DNS-01 Caddy setup, deployment, health checks, and rollback
- [x] 6.3 Add a self-hosted MinIO operations document covering credentials, Bucket policy, CORS, persistence, snapshots or external mirroring, and recovery
- [x] 6.4 Update Rainyun documentation to identify it as the old deployment path and prevent operators from mixing one database with two active Buckets
- [x] 6.5 Update architecture, engineering, media, security, and optimization documentation to reflect internal MinIO traffic and public signed S3 traffic
- [x] 6.6 Update all published demonstration links and examples to include the configured public HTTPS port
- [x] 6.7 Document that FFmpeg remains required by the current media state machine even when uploads are infrequent

## 7. Validate MinIO and Browser Media Flows

- [x] 7.1 Add an integration check that repeated `minio-init` runs preserve the Bucket, application identity, policy, and private access state
- [x] 7.2 Verify anonymous access to upload, processed, moderation, and media object keys returns access denied
- [x] 7.3 Verify exact-origin CORS preflight accepts content type, cache control, checksum, and metadata headers from the configured Frux origin
- [x] 7.4 Verify an unconfigured browser origin does not receive MinIO CORS permission
- [x] 7.5 Verify a browser can complete a presigned video and cover PUT through Caddy and API `HeadObject` validation succeeds
- [x] 7.6 Verify Worker can download a source, publish and verify the deterministic processed object, and complete the durable processing job
- [x] 7.7 Verify public v3 playback returns a bounded signed MinIO redirect with correct Range, HEAD, ETag, and cache behavior
- [x] 7.8 Verify owner and reviewer protected media remains short-lived and `private, no-store`
- [x] 7.9 Restart the stack without deleting volumes and verify database, broker, cache, and MinIO objects remain available
- [x] 7.10 Verify scheduled PostgreSQL backup succeeds and document the configured MinIO snapshot or external-mirror status

## 8. Prepare and Cut Over the Fresh NAT Host

- [x] 8.1 Confirm the allocated NAT ports are fixed, TCP forwarding targets local 443 and 22, Docker is permitted, and the data disk is persistent
- [x] 8.2 Install Docker, Docker Compose, Caddy, curl, OpenSSL, and systemd units on the fresh host
- [x] 8.3 Configure bounded Docker log rotation and verify adequate disk space for images, PostgreSQL, Kafka, MinIO, and backups
- [x] 8.4 Create new JWT, HMAC, internal, PostgreSQL, Redis, MinIO root, and MinIO application secrets in `/opt/frux/.env.prod`
- [x] 8.5 Configure DNS, issue the DNS-01 certificate, load the Caddy routes, and verify both hostnames through the mapped public HTTPS port
- [x] 8.6 Install the GHCR pull agent and deploy the approved digest-pinned release to empty volumes
- [ ] 8.7 Complete registration, login, upload, processing, review, publication, playback, Range seeking, restart, and backup acceptance tests
- [ ] 8.8 Switch public links to the new high-port application origin while leaving the old host and Rainyun Bucket unchanged
- [ ] 8.9 Observe health, memory, disk, MinIO traffic, Worker readiness, and PostgreSQL backups during the acceptance window
- [ ] 8.10 Retain the old deployment for at least 72 hours and roll back public routing without merging new data if acceptance fails

> Archive note (2026-08-19): the stack, TLS, MinIO, persistence, backup, and public-port health checks
> were technically validated, but the NAT provider intercepted public HTTP/TLS traffic with mandatory
> ICP filtering. The user chose not to proceed with备案 or public cutover, so tasks 8.7-8.10 remain
> intentionally incomplete.

## 9. Final Validation

- [x] 9.1 Run targeted backend config, S3 store, upload session, media processing, public media, and deployment tests
- [x] 9.2 Run Go formatting, vet, full tests, and both backend builds
- [x] 9.3 Run frontend lint, tests, and production build
- [x] 9.4 Render and validate local and Prod Compose configurations with all required example variables
- [x] 9.5 Run strict OpenSpec validation and confirm the change is apply-ready
