## 1. Production Rainyun Configuration

- [x] 1.1 Add a secret-free production environment example containing Rainyun credential variable names and placeholders for deployment-owned infrastructure values.
- [x] 1.2 Add a production API configuration that uses endpoint `https://cn-zj1.rains3.com`, bucket `frux1`, path-style S3 access, environment-injected credentials, and the path-style public base URL without copying local credentials into production.
- [x] 1.3 Add a separate production Compose definition that mounts the production configuration, requires production secrets, and does not start or depend on bundled MinIO.
- [x] 1.4 Preserve `apps/docker-compose.yml`, `apps/api/configs/config.docker.yaml`, `minio`, `minio-init`, and the documented default local startup behavior.

## 2. Provider Policy and Operations

- [x] 2.1 Document a Rainyun CORS template requiring the actual production HTTPS Web origin, signed upload methods, required headers, exposed headers, and preflight verification; prohibit wildcard and local-development origins.
- [ ] 2.2 Document and apply a private-by-default bucket policy that grants anonymous `s3:GetObject` only to `arn:aws:s3:::frux1/media/*`; stop rollout if Rainyun cannot enforce that prefix.
- [x] 2.3 Update deployment and startup documentation to distinguish default local MinIO startup from explicit production Rainyun startup, secret injection, and the prohibition on bucket-wide public read.

## 3. Verification

- [x] 3.1 Render and start the unchanged default local Compose configuration and confirm API/Worker still use initialized MinIO without Rainyun variables.
- [x] 3.2 Render the production Compose configuration with non-secret validation placeholders and confirm tracked output contains no credential values or local MinIO dependency.
- [ ] 3.3 After the production HTTPS Web origin and operator-supplied secrets exist, verify Rainyun presigned PUT accepts Frux checksum and metadata headers and that upload completion validates them through `HeadObject`.
- [ ] 3.4 Verify a real production-config video reaches a ready baseline through Worker source read, output write, listing, verification, and cleanup permissions.
- [ ] 3.5 Verify protected Rainyun objects reject anonymous reads, signed protected access succeeds, and promoted `media/*` supports public GET, HEAD, Range, ETag, and bounded cache revalidation.
- [x] 3.6 Run the relevant Go tests, frontend upload tests/build, both Compose configuration validations, and `openspec validate --all --strict`.
