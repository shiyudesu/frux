# simple-server-deployment Specification

## Purpose

Defines the minimal single-server production Compose topology, persistence, backup, reverse-proxy integration, and documented operational limitations.

## Requirements

### Requirement: Minimal Server Compose
Frux SHALL provide a simple `prod` Compose stack containing Web, API, Worker, PostgreSQL, Redis, one
Kafka broker, PostgreSQL backup, MinIO, and an idempotent MinIO initializer.

#### Scenario: Operator renders the prod stack
- **WHEN** required `.env.prod` variables are supplied
- **THEN** Compose renders without three-broker Kafka, monitoring, unresolved secrets, or an external object-storage dependency

#### Scenario: MinIO initialization fails
- **WHEN** the private bucket or application storage identity cannot be initialized
- **THEN** API and Worker do not become ready and the release fails explicitly

### Requirement: Private Internal Services
PostgreSQL, Redis, Kafka, and Worker SHALL publish no host ports. API, Web, MinIO API, and MinIO
Console SHALL bind only to configured `127.0.0.1` ports for the host reverse proxy or operator SSH
tunnel.

#### Scenario: External client scans the server
- **WHEN** an external client connects to the NAT host
- **THEN** Frux data-plane and administration ports are not directly reachable

#### Scenario: Operator opens the MinIO Console
- **WHEN** an operator needs temporary Console access
- **THEN** the operator uses an authenticated SSH tunnel rather than a public Console mapping

### Requirement: Self-Hosted MinIO Media Storage
API and Worker SHALL use a private self-hosted MinIO bucket through the Compose backend network with
path-style addressing, a non-empty signing region, and environment-injected application credentials
that are distinct from MinIO root credentials.

#### Scenario: Local development starts
- **WHEN** the default local Compose command runs
- **THEN** it continues to use the independent development MinIO configuration without production secrets

#### Scenario: Production deployment starts
- **WHEN** the Prod Compose stack receives valid MinIO root and application credentials
- **THEN** the initializer creates or reuses the private bucket and restricts the application identity to that bucket

#### Scenario: Production MinIO credentials are absent
- **WHEN** an operator renders or starts Prod without all required MinIO root and application credentials
- **THEN** deployment fails explicitly before API or Worker becomes ready

### Requirement: Single Internal Kafka
The server stack SHALL use one non-public KRaft Kafka broker with local registered-topic provisioning and SHALL identify this as a non-production-grade durability choice.

#### Scenario: Kafka container is unavailable
- **WHEN** the single Kafka container stops
- **THEN** asynchronous event processing is unavailable until it restarts and no high-availability claim is made

### Requirement: Basic Persistence and Backup
PostgreSQL, Redis, Kafka, and MinIO SHALL use persistent volumes, and PostgreSQL SHALL create
scheduled atomic backups in an operator-controlled persistent location. Documentation SHALL state
that the MinIO volume requires provider snapshots or an external mirror for host-loss recovery.

#### Scenario: Stateless containers are recreated
- **WHEN** API, Worker, Web, MinIO, or initialization containers are recreated without deleting volumes
- **THEN** PostgreSQL, Redis, Kafka, and MinIO state remains available

#### Scenario: Host storage fails
- **WHEN** the NAT host or its only durable disk is lost
- **THEN** the runbook makes no recovery claim beyond available PostgreSQL backups and configured MinIO snapshots or mirrors

### Requirement: Existing Caddy Integration
The server's systemd Caddy SHALL listen on local 443 with a DNS-01-issued certificate, route
API/upload/media/health requests to the loopback API port, route application pages to the loopback
Web port, and route the dedicated S3 hostname to the loopback MinIO API port.

#### Scenario: NAT mappings and DNS are ready
- **WHEN** the allocated public HTTPS high port forwards to local 443 and both DNS hostnames resolve to the NAT address
- **THEN** the application and S3 API are served through HTTPS on the configured public port

#### Scenario: Caddy proxies a signed S3 request
- **WHEN** a browser sends a presigned MinIO PUT or GET through the S3 hostname
- **THEN** Caddy preserves the Host, path, query, and method required by the signature

### Requirement: Explicit Limitations
Documentation SHALL state that the simple server stack is intended for personal or pre-production
use and lacks Kafka authentication, Kafka TLS, Kafka replication, Kafka backup, multi-node MinIO,
independent media fault domains, monitoring dashboards, and server-level high availability.

#### Scenario: Operator needs critical production
- **WHEN** the deployment requires high availability or strict message-broker and object-storage durability
- **THEN** the runbook directs the operator to a separate production-grade architecture rather than silently upgrading this stack

### Requirement: High-Port NAT Public Origin
The Prod deployment SHALL support a configured public HTTPS port that is included in application and
object-storage browser URLs while the NAT gateway forwards that port to host-local 443.

#### Scenario: Public HTTPS port is configured
- **WHEN** the public port is a valid allocated TCP port and the NAT mapping targets local 443
- **THEN** Web, API, cookies, upload origins, and signed S3 URLs operate through the complete high-port origins

#### Scenario: Public HTTPS port is missing or invalid
- **WHEN** Prod configuration cannot construct valid application and S3 HTTPS origins
- **THEN** Compose or application configuration validation fails before the release is accepted

### Requirement: Fresh NAT Host Cutover
The NAT-host runbook SHALL support a fresh deployment with new secrets and empty persistent volumes
without copying existing PostgreSQL, Redis, Kafka, or Rainyun data.

#### Scenario: Fresh deployment is selected
- **WHEN** the operator explicitly accepts that existing data will not migrate
- **THEN** the new host initializes an independent database, broker, cache, and MinIO bucket

#### Scenario: Acceptance validation fails
- **WHEN** the new deployment fails its functional checks during the cutover window
- **THEN** the operator restores the old public destination without deleting the old host or Rainyun bucket
