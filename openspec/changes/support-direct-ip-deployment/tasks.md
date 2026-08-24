## 1. Deployment Configuration

- [x] 1.1 Add backward-compatible public scheme, application/S3 port, bind-address, and HTTPS-enforcement variables to the Prod environment and Compose stack.
- [x] 1.2 Generalize Prod media and presign origins and add direct-IP configuration coverage to API config tests.
- [x] 1.3 Route `/media/` and `/health` through production Web nginx for the direct application entry point.

## 2. Deployment Safety and Verification

- [x] 2.1 Make the server deployment agent validate the access mode and health-check either local Caddy or the direct Web port.
- [x] 2.2 Update MinIO contract validation and CI Compose assertions for default Caddy and explicit direct-IP modes.
- [x] 2.3 Verify Compose rendering, API configuration tests, nginx behavior/build, deployment shell syntax, and strict OpenSpec validation.

## 3. Documentation

- [x] 3.1 Update production, deployment, self-hosted MinIO, security, architecture, engineering, and README guidance for both modes.
- [x] 3.2 Document the direct-IP HTTP risks, exact firewall/port topology, migration steps, and the fact that IP access does not remove applicable ICP filing requirements.
