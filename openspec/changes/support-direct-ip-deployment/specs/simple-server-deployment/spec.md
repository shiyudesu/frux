## MODIFIED Requirements

### Requirement: Private Internal Services
PostgreSQL, Redis, Kafka, and Worker SHALL publish no host ports. API and MinIO Console SHALL bind
only to configured `127.0.0.1` ports. Web and the MinIO API SHALL bind to loopback by default and
MAY bind to an explicitly configured public address only in direct-IP mode.

#### Scenario: External client scans the default server
- **WHEN** an external client connects to a server using the default Caddy mode
- **THEN** Frux data-plane and administration ports are not directly reachable

#### Scenario: External client scans a direct-IP server
- **WHEN** an operator explicitly enables direct-IP mode
- **THEN** only the configured Web and MinIO S3 ports are publicly reachable while API, MinIO Console, PostgreSQL, Redis, Kafka, and Worker remain private

#### Scenario: Operator opens the MinIO Console
- **WHEN** an operator needs temporary Console access in either deployment mode
- **THEN** the operator uses an authenticated SSH tunnel rather than a public Console mapping

## ADDED Requirements

### Requirement: Explicit Direct-IP HTTP Deployment
The Prod deployment SHALL provide a non-default direct-IP mode that serves the application and
browser-presign S3 endpoint on the same IPv4 literal through distinct configured HTTP ports. The
deployment SHALL reject a direct-IP configuration that implicitly retains HTTPS enforcement, uses
different public hosts, or assigns the same port to both endpoints.

#### Scenario: Operator enables direct-IP mode
- **WHEN** the operator supplies one public IPv4 literal, distinct application and S3 ports, the HTTP scheme, disabled public-HTTPS enforcement, and the explicit public bind address
- **THEN** Web/API/media are reachable through the application origin and presigned MinIO requests are reachable through the S3 origin

#### Scenario: Operator supplies an inconsistent direct-IP configuration
- **WHEN** the selected mode, scheme, hosts, ports, bind address, or public-HTTPS enforcement do not form a valid direct-IP topology
- **THEN** deployment fails before the release is accepted

#### Scenario: Existing Caddy environment is reused
- **WHEN** an existing operator has only the legacy domain, S3 domain, and public HTTPS port variables
- **THEN** Prod continues to render with HTTPS enforcement, a shared public port, and loopback Web and MinIO bindings

### Requirement: Direct Application Entry Point
The production Web proxy SHALL route application API, upload, public-media, and health requests to
the private API service so the complete application origin works with or without host Caddy.

#### Scenario: Direct-IP browser requests application routes
- **WHEN** a browser uses `/api/`, `/uploads/`, `/media/`, or `/health` through the public application port
- **THEN** Web nginx preserves the request path and forwards it to the private API service
