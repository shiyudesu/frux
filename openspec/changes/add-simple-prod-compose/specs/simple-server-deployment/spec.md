## ADDED Requirements

### Requirement: Minimal Server Compose
Frux SHALL provide a simple `prod` Compose stack containing Caddy, Web, API, Worker, PostgreSQL, Redis, one Kafka broker, and PostgreSQL backup while using Rainyun for media.

#### Scenario: Operator renders the prod stack
- **WHEN** required `.env.prod` variables are supplied
- **THEN** Compose renders without MinIO, three-broker Kafka, monitoring, or unresolved secrets

### Requirement: Private Internal Services
Only Caddy SHALL publish public host ports; PostgreSQL, Redis, Kafka, API, Worker, and Web SHALL remain reachable only through Compose networks.

#### Scenario: External client scans the server
- **WHEN** an external client connects to the server
- **THEN** only HTTP/HTTPS entry ports are publicly reachable

### Requirement: Rainyun Media Storage
API and Worker SHALL use Rainyun endpoint `https://cn-zj1.rains3.com`, bucket `frux1`, path-style access, and environment-injected credentials.

#### Scenario: Local development starts
- **WHEN** the default local Compose command runs
- **THEN** it continues to use local MinIO without requiring Rainyun variables

### Requirement: Single Internal Kafka
The server stack SHALL use one non-public KRaft Kafka broker with local registered-topic provisioning and SHALL identify this as a non-production-grade durability choice.

#### Scenario: Kafka container is unavailable
- **WHEN** the single Kafka container stops
- **THEN** asynchronous event processing is unavailable until it restarts and no high-availability claim is made

### Requirement: Basic Persistence and Backup
PostgreSQL, Redis, and Kafka SHALL use persistent volumes, and PostgreSQL SHALL create scheduled atomic backups in an operator-selected host directory.

#### Scenario: Stateless containers are recreated
- **WHEN** API, Worker, Web, or Caddy is recreated without deleting volumes
- **THEN** PostgreSQL, Redis, and Kafka state remains available

### Requirement: Simple HTTPS Deployment
Caddy SHALL obtain HTTPS certificates for the configured domain and route API/upload/health requests to API and application pages to Web.

#### Scenario: Production domain is ready
- **WHEN** DNS points to the server and ports 80/443 are open
- **THEN** the application is served through HTTPS

### Requirement: Explicit Limitations
Documentation SHALL state that the simple server stack is intended for personal or pre-production use and lacks Kafka authentication, Kafka TLS, Kafka replication, Kafka backup, monitoring dashboards, and server-level high availability.

#### Scenario: Operator needs critical production
- **WHEN** the deployment requires high availability or strict message-broker security
- **THEN** the runbook directs the operator to a separate production-grade architecture rather than silently upgrading this stack
