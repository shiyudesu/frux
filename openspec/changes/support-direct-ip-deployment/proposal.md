## Why

The current Prod topology requires two DNS hostnames and a host-level HTTPS reverse proxy, so an operator cannot perform a bounded public-IP deployment or connectivity test before DNS and certificate setup is available. Frux needs an explicit, non-default direct-IP mode without weakening the existing domain/HTTPS deployment or exposing internal services.

## What Changes

- Add an explicit direct-IP HTTP deployment mode with separate public application and S3 ports on the same IP address.
- Generalize the configured application and browser-presign origins so domain/HTTPS and IP/HTTP deployments use the same API and MinIO contracts.
- Keep PostgreSQL, Redis, Kafka, Worker, API, and MinIO Console private; direct-IP mode publishes only Web and the MinIO S3 API.
- Extend the Web proxy, deployment health gate, MinIO validation, CI, and runbooks for both deployment modes.
- Document that direct-IP HTTP is an insecure personal/pre-production option and does not remove any applicable ICP filing or provider requirements.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `simple-server-deployment`: Support an explicit direct-IP HTTP topology with separate application and S3 ports while preserving private internal services and the default Caddy HTTPS topology.
- `ghcr-prod-deployment`: Make server-side route health checks mode-aware without weakening rollback gates.
- `production-media-delivery`: Allow an explicitly configured HTTP browser-presign origin for direct-IP mode while preserving exact-origin CORS, private buckets, scoped credentials, and HTTPS-by-default validation.

## Impact

- Prod environment variables, Compose port bindings, API media configuration, Web nginx routing, and deployment health checks.
- MinIO CORS and contract validation scripts.
- CI deployment validation and the GHCR deployment bundle inputs.
- Deployment, security, architecture, and self-hosted MinIO documentation.
