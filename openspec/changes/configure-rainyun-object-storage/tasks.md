## 1. Production Rainyun Configuration

- [x] 1.1 Add a secret-free production environment example containing Rainyun credential variable names and placeholders for deployment-owned infrastructure values.
- [x] 1.2 Add a production API configuration that uses endpoint `https://cn-zj1.rains3.com`, bucket `frux1`, path-style S3 access, environment-injected credentials, and the Frux public-media base URL without copying local credentials into production.
- [x] 1.3 Add a separate production Compose definition that mounts the production configuration, requires production secrets, and does not start or depend on bundled MinIO.
- [x] 1.4 Preserve `apps/docker-compose.yml`, `apps/api/configs/config.docker.yaml`, `minio`, `minio-init`, and the documented default local startup behavior.

## 2. Provider Policy and Operations

- [x] 2.1 Document Rainyun's provider-wide wildcard CORS behavior and the OPTIONS verification for Frux signed upload headers.
- [x] 2.2 Keep `frux1` private and implement stable authorized public-media URLs: serve MPD/HEAD through Frux and redirect media-byte GETs to short-lived Rainyun signed URLs.
- [x] 2.3 Update deployment and startup documentation to distinguish default local MinIO startup from explicit production Rainyun startup, secret injection, and the prohibition on bucket-wide public read.

## 3. Verification

- [x] 3.1 Render and start the unchanged default local Compose configuration and confirm API/Worker still use initialized MinIO without Rainyun variables.
- [x] 3.2 Render the production Compose configuration with non-secret validation placeholders and confirm tracked output contains no credential values or local MinIO dependency.
- [x] 3.3 Implement and test S3 public-media routing, router wiring, stable public base URL, authorization denial, MPD/HEAD serving, Range-preserving GET redirects, and no-store signed responses.
- [x] 3.4 Route `/media/*` through the existing host Caddy to API and verify direct anonymous Rainyun object access remains denied.
- [ ] 3.5 After operator-supplied secrets exist, verify Rainyun presigned PUT accepts checksum/metadata headers and upload completion validates through `HeadObject`.
- [ ] 3.6 Verify a real Prod video reaches a ready baseline and public playback follows Frux authorization to a signed Rainyun GET with Range, HEAD, and ETag.
- [x] 3.7 Run the relevant Go tests, frontend upload tests/build, Compose validations, and `openspec validate --all --strict`.
