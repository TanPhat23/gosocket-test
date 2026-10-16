# Specification: Gate 0F Production Deployment, Secret Bootstrap & Licensing Design

**Status:** PROPOSED FOR PLANNING (Design Direction Directionally Selected; Security Review & Checkpoint A/B Pending; Implementation Governed by Downstream Hardening Plan)  
**Gate Status:** UNQUALIFIED (Blocks Production Release; Qualification Probes 0F-1 through 0F-4 Execute in Task 8)  
**Formal Review & User Sign-off:** PENDING FORMAL SECURITY SIGN-OFF & QUALIFICATION (User chose MIT License, catalog versions, in-memory Dex storage v1, and subagent execution approach during directional planning; independent security sign-off, host qualification, and formal Checkpoints A/B remain pending)  
**Author:** Documentation Implementation Specialist  
**Date:** 2026-10-12 (Reconciled Planning Record 2026-10-13)  
**Target Platform:** Single-Host Linux (`linux/amd64`), Native Docker Engine, Docker Compose (pinned v2.x), Host Operator-Managed Firewall  
**Specification File:** `docs/superpowers/specs/2026-10-11-gate-0f-production-deployment-design.md`  

---

## 1. Executive Summary & Problem Statement

This specification defines the target production topology, ingress decoupling, secret management contract, configuration validation, repository licensing resolution, and reproducible qualification probes for **Gate 0F: Production Deployment, Secret Bootstrap & Licensing**.

Under the self-hosting architecture outlined in `docs/superpowers/specs/2026-10-11-self-hosting-readiness-design.md`, Agent Task Manager (ATM) operates on a single Linux host (`linux/amd64`). Untrusted AI agent sessions and task workloads execute within unprivileged, capability-dropped project sandbox containers (`atm-project-<project_id>`).

### Current Implementation Deficiencies & Insecure Defaults

The current repository configuration contains numerous development shortcuts and security vulnerabilities that prevent secure production deployment:

1. **Direct Traefik Docker Socket Mount:**
   `docker-compose.yml:34` mounts `/var/run/docker.sock:/var/run/docker.sock:ro` into the public-facing Traefik container. While mounted read-only, read access to the Docker daemon socket permits querying all container metadata, inspect payloads, environment variables (often holding secrets), and execution status across the entire host. In Docker Engine, `/var/run/docker.sock` is a root-equivalent control plane interface. The `:ro` mount flag does **not** establish an authorization boundary.
2. **Exposed Control Plane Docker Daemon Privileges:**
   `docker-compose.yml:134` mounts the host Docker socket `/var/run/docker.sock:/var/run/docker.sock` (writable) directly into `control-plane`. This gives the control plane root-equivalent authority on the host daemon. While necessary today for container orchestration, ingress proxy compromise must never lead to Docker daemon compromise.
3. **Pervasive Hardcoded Development Secrets:**
   `docker-compose.yml` embeds insecure fallback credentials:
   - PostgreSQL: `POSTGRES_USER=postgres`, `POSTGRES_PASSWORD=postgres` (lines 47-48, 112-113, 116)
   - Redis: `REDIS_PASSWORD=secretredis` (lines 63, 69, 118)
   - AES-GCM Master Key: `SECRET_ENCRYPTION_KEY=01234567890123456789012345678901` (line 119)
   - OIDC Auth: `OIDC_CLIENT_SECRET=dev-oidc-secret-change-in-prod-1234` (line 123)
   - Web Sessions: `SESSION_SECRET=dev-session-secret-change-in-prod-1234` (line 125)
   - OpenCode Transport: `OPENCODE_PASSWORD=4YXQ3udRl8k_UkFdyhHSFF-bdInQgKk0F9Qz6zp2y0U` (line 131)
4. **Host Directory Leakage and Bind Mounts:**
   `docker-compose.yml:128-139` hardcodes developer home paths (`/home/ghug/...`) into `HOST_WORKTREE_ROOT`, `HOST_REPO_ROOT`, and OpenCode configuration/data mounts, bypassing isolated container storage.
5. **Host-Networking Bridge:**
   `docker-compose.yml:171-178` launches `opencode-bridge` with `network_mode: host` and `mode=777` socket permissions, breaking network isolation.
6. **TLS Verification Disabled:**
   `docker-compose.yml:132` sets `GIT_SSL_NO_VERIFY=true`.
7. **Undefined Repository License:**
   The repository `README.md` displays an MIT badge (`[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)`), yet no tracked `LICENSE` file exists in the repository tree.

### Core Objectives of Gate 0F

- **Traefik Socket Decoupling:** Completely eliminate the Docker socket mount from Traefik by migrating to a static/templated file-based dynamic routing provider, or alternatively routing via a tightly scoped, least-privileged Docker socket filter proxy.
- **Production Secret Bootstrap Contract:** Define a secure, automated bootstrap tool to generate high-entropy secrets, store them in restricted-permission (`0600`) `.env` files or file-backed Compose secrets, ensure atomic creation with symlink/clobber protection, and reject printing secrets to stdout or daemon logs.
- **Strict Startup Fail-Closed Validation:** Enforce deterministic startup validation in the control plane that immediately halts boot if known test/example secrets or insecure defaults are detected.
- **Explicit Encryption Key Encoding & Rotation Specification:** Standardize `SECRET_ENCRYPTION_KEY` representation rules, length validation, non-secret error reporting, and migration/backup workflows without silent data loss.
- **Repository License & Runtime Catalog Resolution:** Formally register the repository license requirement and the exact runtime catalog approval decision without resolving them unilaterally.

> **CRITICAL ARCHITECTURAL BOUNDARY:**
> This document is a **design specification only**. It does **not** implement Compose modifications, modify application source code, execute host or Docker commands, alter firewall/VM configuration, or commit git changes. Gate 0F remains **UNQUALIFIED** until formal operator review and qualification probes pass under Checkpoint A.

---

## 2. Present Implementation vs. Proposed Target State

| Architectural Area | Present Implementation (Insecure Baseline) | Proposed Target State (Gate 0F Production) |
|---|---|---|
| **Traefik Ingress Topology** | Mounts `/var/run/docker.sock:ro`. Discovers routing via Docker container labels. | **Docker socket eliminated.** Traefik uses strictly file-based configuration (`providers.file`). Ingress routes are declared in static files. |
| **Ingress Container Privileges** | Direct access to host Docker daemon API. | Zero Docker API access. Pure network reverse proxy on `agent_ingress`. |
| **Control Plane Docker Access** | Mounts `/var/run/docker.sock:rw`. Manages containers directly. | Retains Docker socket access for orchestration, but isolated from public ingress. Ingress compromise does not grant Docker socket. |
| **Secret Provisioning** | Hardcoded default secrets interpolated or defaulted directly in `docker-compose.yml`. | Environment file `.env` generated with `mode 0600` via bootstrap CLI; or Compose file secrets (`/run/secrets/*`). |
| **Default Secret Detection** | Startup proceeds silently with insecure test credentials. | Control plane startup preflight check validates secrets against known defaults and halts boot with exit code `1`. |
| **`SECRET_ENCRYPTION_KEY` Format** | Raw string read via `os.Getenv`; converted via `[]byte(...)`; checks `len == 32`. No hex/base64 support. | Evaluated alternatives: retains raw string or introduces typed hex/base64 decoding with backwards compatibility; strict non-secret errors. |
| **Host Directory Binds** | Host paths (`/home/ghug/...`) mounted directly into control plane and bridge. | Dedicated Docker named volumes (`postgres_data`, `redis_data`, `atm-project-data-*`). Zero developer host bind mounts. |
| **Network Isolation** | Uses `network_mode: host` for `opencode-bridge`. Ingress bridge shared across services. | Segmented bridge networks: `agent_ingress` (public Traefik to UI/Control Plane/Dex) and `agent_backend` (internal only). No host networking. |
| **TLS & Host Firewall** | `GIT_SSL_NO_VERIFY=true`. Ingress ports `80`/`443` bound directly; host firewall assumptions implicit. | Strict TLS verification (`GIT_SSL_NO_VERIFY=false` / unset). Host operator manages external firewall (ports 80/443 exposed, DB/Redis blocked). |
| **Repository License** | README displays MIT badge; no `LICENSE` file exists in tree. | Explicit license decision gate logged; maintainer/user decision required. |
| **Runtime Catalog** | Catalog versions approved in runtime spec §3.1, but readiness spec §9.5 lists catalog approval as open. | Source-status conflict explicitly recorded in decision gate. Pinned baseline candidate versions documented. |

---

## 3. Production Compose Topology & Network Architecture

### 3.1 Network Segmentation & Service Isolation

The production deployment divides the architecture into two distinct Docker bridge networks:
1. `agent_ingress` (`driver: bridge`): External-facing network. Traefik connects to this network alongside the ingress-facing endpoints: `web-ui`, `control-plane` (API/WS routes), and `dex` (OIDC provider).
2. `agent_backend` (`driver: bridge`, `internal: true`): Isolated internal network. Holds data stores (`postgres`, `redis`) and control plane backend communication. `traefik` is **never** attached to `agent_backend`.

```
 Internet (Clients / Browsers)
           │
           │ TCP 80, 443
           ▼
┌────────────────────────────────────────────────────────────────────────┐
│ Host Operator Firewall (nftables / iptables)                           │
│ - Permits inbound: 80, 443                                             │
│ - Blocks external access to internal ports (5432, 6379, 8080, 5556)    │
└──────────────────────────────────┬─────────────────────────────────────┘
                                   │
                                   ▼
┌────────────────────────────────────────────────────────────────────────┐
│                        traefik (Reverse Proxy)                         │
│ - Network: agent_ingress ONLY                                          │
│ - Ports: 80:80, 443:443                                                │
│ - Volumes: deploy/traefik/dynamic.yml (ro), traefik_acme               │
│ - ZERO Docker Socket Mount (/var/run/docker.sock PROHIBITED)           │
└──────────────────┬─────────────────┬───────────────────┬───────────────┘
                   │                 │                   │
   agent_ingress   │                 │                   │
   Network         ▼                 ▼                   ▼
            ┌──────────────┐  ┌──────────────┐    ┌──────────────┐
            │    web-ui    │  │control-plane │    │     dex      │
            │  (Port 3000) │  │ (Port 8080)  │    │ (Port 5556)  │
            └──────────────┘  └──────┬───────┘    └──────┬───────┘
                                     │                   │
   agent_backend                     │ (internal: true)  │
   Network                           ▼                   ▼
                            ┌────────────────┐   ┌──────────────┐
                            │    postgres    │   │    redis     │
                            │  (Port 5432)   │   │ (Port 6379)  │
                            └────────────────┘   └──────────────┘
```

### 3.2 Traefik Docker Socket Decoupling & Static Configuration Contract

#### Preferred Approach: Static File-Based Dynamic Routing
Traefik's dynamic configuration is migrated completely to `deploy/traefik/dynamic.yml` using Traefik's `file` provider. Traefik has **zero** container socket mounts. The Docker provider (`providers.docker`) is completely omitted from the static configuration rather than set to null, guaranteeing that Traefik cannot attempt Docker socket discovery.

- Complete Traefik static configuration contract (`deploy/traefik/traefik.yml`):
  ```yaml
  global:
    checkNewVersion: false
    sendAnonymousUsage: false

  entryPoints:
    web:
      address: ":80"
      http:
        redirections:
          entryPoint:
            to: websecure
            scheme: https
            permanent: true
    websecure:
      address: ":443"
      http:
        tls:
          certResolver: letsencrypt

  certificatesResolvers:
    letsencrypt:
      acme:
        # NOTICE: In Traefik static configuration, environment variables in the file itself
        # are not interpolated by Docker Compose. The authoritative Let's Encrypt account email
        # is supplied via Traefik environment override configured in docker-compose.prod.yml:
        # TRAEFIK_CERTIFICATESRESOLVERS_LETSENCRYPT_ACME_EMAIL=${ACME_EMAIL}
        storage: /letsencrypt/acme.json
        httpChallenge:
          entryPoint: web

  # NOTICE: 'providers.docker' is completely omitted to eliminate Docker socket access.
  providers:
    file:
      filename: /etc/traefik/dynamic.yml
      watch: true
  ```
- Services are routed by Docker service names resolvable via embedded DNS on `agent_ingress`:
  - `web-ui`: `http://web-ui:3000`
  - `control-plane`: `http://control-plane:8080`
  - `dex`: `http://dex:5556`

#### Evaluated Alternative: Constrained Read-Only Docker Socket Proxy
If dynamic container discovery is required in future multi-container ingress expansions, direct mounting of `/var/run/docker.sock` remains strictly forbidden. Instead, an intermediary proxy container (e.g. `tecnativa/docker-socket-proxy`) must be deployed with a least-privileged API allowlist:
- `CONTAINERS=1` (allow `GET /containers/json`)
- All other endpoints disabled (`POST=0`, `DELETE=0`, `EXEC=0`, `VOLUMES=0`, `NETWORKS=0`, `SWARM=0`, `SYSTEM=0`, `AUTH=0`, `SECRETS=0`)
- **Security Assessment:** Even with filtered GET endpoints, container metadata and labels could leak operational information. Furthermore, socket `:ro` alone is **not** an authorization boundary. Therefore, **the static file-based routing provider is the primary recommended design**.

### 3.3 Proposed Production `docker-compose.prod.yml` Schema Example

> **IMAGE VERSION PINNING & QUALIFICATION NOTICE:**  
> The container images and tags referenced below (`traefik:v3.1`, `postgres:18-alpine`, `redis:7-alpine`, `dexidp/dex:v2.41.1`, and application `:latest` tags) are **candidate proposed versions only**. None of these images or tags are digest-qualified or approved for production release. Pinning exact immutable container image digests (`image@sha256:...`) and verifying multi-architecture build provenance are required qualification milestones prior to production sign-off.

```yaml
version: "3.8"

networks:
  ingress:
    name: agent_ingress
    driver: bridge
  backend:
    name: agent_backend
    driver: bridge
    internal: true

volumes:
  postgres_data:
    name: agent_postgres_data
  redis_data:
    name: agent_redis_data
  traefik_acme:
    name: agent_traefik_acme

services:
  traefik:
    image: traefik:v3.1
    container_name: agent-traefik
    restart: unless-stopped
    command:
      - "--configFile=/etc/traefik/traefik.yml"
    environment:
      - DOMAIN=${DOMAIN}
      - TRAEFIK_CERTIFICATESRESOLVERS_LETSENCRYPT_ACME_EMAIL=${ACME_EMAIL}
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - "./deploy/traefik/traefik.yml:/etc/traefik/traefik.yml:ro"
      - "./deploy/traefik/dynamic.yml:/etc/traefik/dynamic.yml:ro"
      - "traefik_acme:/letsencrypt"
    networks:
      - ingress
    # NOTICE: /var/run/docker.sock is NOT mounted here.

  postgres:
    image: postgres:18-alpine
    container_name: agent-postgres
    restart: unless-stopped
    environment:
      POSTGRES_DB: ${POSTGRES_DB}
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      # Official PostgreSQL 18+ images define PGDATA as version-specific (/var/lib/postgresql/18/docker)
      # and the base declared VOLUME as /var/lib/postgresql.
      PGDATA: /var/lib/postgresql/18/docker
    volumes:
      - postgres_data:/var/lib/postgresql
    networks:
      - backend
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $$POSTGRES_USER -d $$POSTGRES_DB"]
      interval: 5s
      timeout: 5s
      retries: 5

  redis:
    image: redis:7-alpine
    container_name: agent-redis
    restart: unless-stopped
    # Redis configuration is supplied via a configuration file mount to avoid exposing passwords in process arguments (argv)
    command: ["redis-server", "/usr/local/etc/redis/redis.conf"]
    volumes:
      - redis_data:/data
      - ./deploy/redis/redis.conf:/usr/local/etc/redis/redis.conf:ro
      - ./deploy/redis/redis-healthcheck.sh:/usr/local/bin/redis-healthcheck.sh:ro
    networks:
      - backend
    healthcheck:
      # Healthcheck invokes dedicated helper mounted read-only, reading credentials from file
      test: ["CMD", "/usr/local/bin/redis-healthcheck.sh"]
      interval: 5s
      timeout: 5s
      retries: 5

  dex:
    image: dexidp/dex:v2.41.1
    container_name: agent-dex
    restart: unless-stopped
    # Proposed production Dex config generated at bootstrap from deploy/dex/config.prod.yml.template
    # Replaces development client secrets and removes hardcoded 'password123' static accounts
    volumes:
      - ./deploy/dex/config.prod.yml:/etc/dex/config.docker.yaml:ro
    networks:
      - ingress
      - backend

  control-plane:
    image: agent-control-plane:latest
    container_name: agent-control-plane
    restart: unless-stopped
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
      dex:
        condition: service_started
    environment:
      APP_ENV: production
      PORT: "8080"
      DATABASE_HOST: postgres
      DATABASE_PORT: "5432"
      DATABASE_USER: ${POSTGRES_USER}
      DATABASE_PASSWORD: ${POSTGRES_PASSWORD}
      DATABASE_NAME: ${POSTGRES_DB}
      DATABASE_SSLMODE: disable
      DATABASE_URL: "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable"
      REDIS_ADDR: redis:6379
      REDIS_PASSWORD: ${REDIS_PASSWORD}
      SECRET_ENCRYPTION_KEY: ${SECRET_ENCRYPTION_KEY}
      OIDC_ISSUER_URL: https://${DOMAIN}/dex
      OIDC_INTERNAL_ISSUER_URL: http://dex:5556/dex
      OIDC_CLIENT_ID: ${OIDC_CLIENT_ID}
      OIDC_CLIENT_SECRET: ${OIDC_CLIENT_SECRET}
      OIDC_REDIRECT_URL: https://${DOMAIN}/api/v1/auth/callback
      SESSION_SECRET: ${SESSION_SECRET}
      MAX_CONCURRENT_TASKS: ${MAX_CONCURRENT_TASKS:-4}
      TASK_TIMEOUT_MINUTES: ${TASK_TIMEOUT_MINUTES:-60}
      GIT_SSL_NO_VERIFY: "false"
    volumes:
      - "/var/run/docker.sock:/var/run/docker.sock"
    networks:
      - ingress
      - backend

  web-ui:
    image: agent-web-ui:latest
    container_name: agent-web-ui
    restart: unless-stopped
    environment:
      INTERNAL_API_URL: http://control-plane:8080
    networks:
      - ingress
```

### 3.4 Proposed Production Dynamic Traefik Routes (`deploy/traefik/dynamic.yml`)

Under Traefik routing rules, descending rule length determines router matching priority by default (e.g., path-specific rules take precedence over host-only rules). To make route precedence fully explicit, deterministic, and resilient across edge cases or subpath expansion, the configuration declares explicit `priority` integers: specific API/WS and Dex routes receive `priority: 100`, while the host root UI catch-all receives `priority: 10`.

```yaml
http:
  middlewares:
    security-headers:
      headers:
        sslRedirect: true
        hostsProxyHeaders:
          - "X-Forwarded-Host"
        customResponseHeaders:
          X-Content-Type-Options: "nosniff"
          X-Frame-Options: "DENY"
          X-XSS-Protection: "1; mode=block"
          Referrer-Policy: "strict-origin-when-cross-origin"
          Permissions-Policy: "camera=(), microphone=(), geolocation=()"

  routers:
    web-ui:
      rule: 'Host(`{{ env "DOMAIN" }}`)'
      entryPoints:
        - websecure
      priority: 10
      middlewares:
        - security-headers
      service: web-ui-service

    control-plane:
      rule: 'Host(`{{ env "DOMAIN" }}`) && (PathPrefix(`/api/v1`) || PathPrefix(`/ws`))'
      entryPoints:
        - websecure
      priority: 100
      middlewares:
        - security-headers
      service: control-plane-service

    dex:
      rule: 'Host(`{{ env "DOMAIN" }}`) && PathPrefix(`/dex`)'
      entryPoints:
        - websecure
      priority: 100
      service: dex-service

  services:
    web-ui-service:
      loadBalancer:
        servers:
          - url: "http://web-ui:3000"

    control-plane-service:
      loadBalancer:
        servers:
          - url: "http://control-plane:8080"

    dex-service:
      loadBalancer:
        servers:
          - url: "http://dex:5556"
```

### 3.5 Proposed Production Service Hardening: Redis & Dex Authentication Contracts

#### Redis Production Authentication & Healthcheck

> **DESIGN NOTICE (PROPOSED FUTURE ARTIFACTS):**  
> Neither `./deploy/redis/redis.conf` nor `./deploy/redis/redis-healthcheck.sh` exists in the repository today. They represent proposed production artifacts to be created during Task 5 / Task 6 implementation.

In the production Compose model:
1. **Configuration Mount & Rendering:** `redis` configuration is supplied via a configuration file mount (`./deploy/redis/redis.conf:/usr/local/etc/redis/redis.conf:ro`) rendered at bootstrap from a template (`./deploy/redis/redis.conf.template`) containing `requirepass <REDIS_PASSWORD>`. Password values are not passed as command-line arguments (`--requirepass`).
2. **Healthcheck Helper Script:** The container healthcheck invokes `/usr/local/bin/redis-healthcheck.sh`, mounted read-only from `./deploy/redis/redis-healthcheck.sh:/usr/local/bin/redis-healthcheck.sh:ro`. This script executes `redis-cli ping` reading credentials from the mounted configuration or a private container secret file, completely eliminating secret strings from `docker inspect` command text or inspectable Compose definitions.
   - *Fail-Closed Authentication Enforcement:* The healthcheck helper strictly validates that `/usr/local/etc/redis/redis.conf` contains **exactly one** valid, non-empty `requirepass` directive using POSIX character classes (`[[:space:]]`). If the directive is missing, empty, duplicated, or unparseable, the script aborts immediately with a non-zero exit code (`1`), preventing a misconfigured unauthenticated Redis server from falsely reporting healthy:
   ```bash
   #!/bin/sh
   # Proposed deploy/redis/redis-healthcheck.sh (mode 0755)
   set -eu

   CONF="/usr/local/etc/redis/redis.conf"
   if [ ! -f "$CONF" ]; then
     echo "REDIS HEALTHCHECK ERROR: Configuration file not found" >&2
     exit 1
   fi

   # Extract all uncommented requirepass lines using portable POSIX whitespace matching
   MATCHES=$(grep -E '^[[:space:]]*requirepass[[:space:]]+' "$CONF" || true)
   COUNT=$(echo "$MATCHES" | grep -c -E '^[[:space:]]*requirepass[[:space:]]+' || true)

   if [ "$COUNT" -ne 1 ]; then
     echo "REDIS HEALTHCHECK ERROR: Expected exactly 1 requirepass directive, found $COUNT" >&2
     exit 1
   fi

   # Parse the password argument safely without echoing secret values
   PASS=$(echo "$MATCHES" | awk '{print $2}')
   if [ -z "$PASS" ]; then
     echo "REDIS HEALTHCHECK ERROR: Extracted requirepass directive is empty" >&2
     exit 1
   fi

   # Execute authenticated ping; output is discarded to prevent secret or status leak
   REDISCLI_AUTH="$PASS" exec redis-cli ping >/dev/null
   ```

#### Dex Production Identity Configuration & State Management

> **DESIGN NOTICE (PROPOSED FUTURE ARTIFACTS & OPERATOR PREREQUISITE):**  
> `deploy/dex/config.prod.yml` does not exist in the repository today and must be rendered at bootstrap from `deploy/dex/config.prod.yml.template`. Furthermore, Dex upstream enterprise identity connectors must be explicitly configured by the self-hosting operator before enabling production logins.

The current development Dex configuration (`deploy/dex/config.yml`) hardcodes `issuer: http://localhost/dex`, client secret `'dev-oidc-secret-change-in-prod-1234'`, and static test accounts with bcrypt hashes for `'password123'`.
For production deployment:
1. **Template Rendering & Host Permission Contract:** Dex configuration is generated at bootstrap as `deploy/dex/config.prod.yml` via template substitution (`deploy/dex/config.prod.yml.template`).
   - *Container Process UID Compatibility:* The upstream official Dex container image (`dexidp/dex:v2.41.1`) executes under an unprivileged user (`USER 1001:1001`). If a host file owned by the host operator is mounted read-only with permissions `0600`, non-root user `1001` cannot read the configuration (`permission denied`).
   - *Least-Privilege Host Contract (Preventing World-Readable Secrets):* Rendering secrets with world-readable permissions (`0644` in standard shared directories) exposes production client secrets to any unprivileged local user or process on the host. Therefore, `config.prod.yml` must **not** be world-readable. The production host contract requires:
     1. Dedicated configuration directories (`deploy/dex`, `deploy/redis`) restricted strictly to mode `0750` owned by the operator and grouped to the service container's GID (`1001` for Dex, `999` for Redis).
     2. Configuration files rendered with mode `0640` and group-owned by the container service GID.
     3. *Fail-Closed Ownership Prerequisite Validation:* The bootstrap script checks effective ownership and group readability. If running as non-root and the operator user does not belong to GID 1001 / 999 or cannot establish service group access, the script fails closed with an explicit error instructing the operator to run a privileged permission setup (`sudo chgrp -R 1001 deploy/dex && sudo chgrp -R 999 deploy/redis`). Master credentials in `.env` remain strictly mode `0600`.
2. **Issuer & Client Credentials:** The issuer URL is set strictly to `https://${DOMAIN}/dex`. Client secrets are populated from `${OIDC_CLIENT_SECRET}`.
3. **Elimination of Static Passwords:** Static test passwords (`admin@example.com` / `developer@example.com` with `password123`) and `enablePasswordDB` are **strictly disabled** (`enablePasswordDB: false`).
4. **Mandatory Operator Upstream Connectors:** Production authentication requires the self-hosting operator to configure upstream enterprise IdP connectors (e.g. GitHub OAuth, Google, SAML, or LDAP) in `connectors: []`. No fallback default credentials or built-in test users are permitted in production.
5. **Storage Architecture Decision (Memory vs. Persistent Database):**
   - *Current Staging Baseline:* Dex is configured with `storage: type: memory`. In-memory storage means Dex loses ephemeral session state and active auth codes across container restarts. While acceptable for stateless OIDC token verification where tokens are short-lived, it causes active authentication flows to be invalidated upon restart.
   - *Production Recommendation:* For durable multi-instance or zero-downtime production deployments, Dex should configure PostgreSQL storage (`type: postgres`) targeting `agent_backend`. This decision is tracked as an operator deployment option and remains scoped to Checkpoint A review.

```yaml
# Proposed deploy/dex/config.prod.yml.template
issuer: https://${DOMAIN}/dex

storage:
  type: memory

web:
  http: 0.0.0.0:5556

telemetry:
  http: 0.0.0.0:5558

frontend:
  theme: "dark"
  issuer: "Agent Task Manager"

oauth2:
  responseTypes: ["code"]
  skipApprovalScreen: true

staticClients:
  - id: agent-control-plane
    redirectURIs:
      - 'https://${DOMAIN}/api/v1/auth/callback'
    name: 'Agent Task Manager'
    secret: '${OIDC_CLIENT_SECRET}'

enablePasswordDB: false
# Enterprise identity connectors configured by operator (e.g., GitHub, Google, LDAP)
connectors: []
```

---

## 4. Secret Inventory & Production Bootstrap Contract

### 4.1 Production Secret Inventory

| Secret Variable | Target Consumer | Purpose & Scope | Minimum Entropy / Format |
|---|---|---|---|
| `POSTGRES_PASSWORD` | PostgreSQL, Control Plane | Database authentication | ≥ 32 alphanumeric/hex chars |
| `REDIS_PASSWORD` | Redis, Control Plane | Cache & queue authentication | ≥ 32 alphanumeric/hex chars |
| `SECRET_ENCRYPTION_KEY` | Control Plane | AES-256-GCM encryption of stored integrations/PATs | Exactly 256 bits (32 bytes literal or 64 hex chars) |
| `SESSION_SECRET` | Control Plane | Cookie signature & web session auth | ≥ 32 alphanumeric/base64 characters (minimum 32 bytes) |
| `OIDC_CLIENT_SECRET` | Dex, Control Plane | OIDC OAuth2 client authentication | ≥ 32 alphanumeric chars |
| `ACME_EMAIL` | Traefik | Let's Encrypt account notification | Valid operator email address |
| `DOMAIN` | Traefik, Dex, Control Plane | Public FQDN | Valid FQDN hostname |

### 4.2 Local File-Backed Secrets & Host Threat Boundary

> **SECURITY NOTICE: COMPOSE FILE SECRETS DO NOT CONSTITUTE ENCRYPTION-AT-REST**  
> In Docker Compose on a single Linux host, secrets defined as local files (`secrets: [ ... ]`) or `.env` variables are mounted as standard Linux files or passed as environment variables.
> 1. Local Compose file secrets **do not protect against host root or docker daemon operators**. Any user with root privileges or access to `/var/run/docker.sock` can read container filesystems, memory, and mounted secrets.
> 2. Compose file secrets **are not encrypted at rest** on the host filesystem unless the host operator employs full-disk encryption (e.g. LUKS) or encrypted volumes.
> 3. Values placed in `.env` files are interpolated by Docker Compose and visible in `docker compose config` output and container inspection if mapped to environment variables.
> 4. Therefore, the security boundary relies strictly on **host-level file permissions (`0600`), operator trust, and strict non-exposure via logs or stdout**.

### 4.3 Proposed Production Bootstrap Contract (`scripts/bootstrap-secrets.sh`)

> **SPECIFICATION NOTICE (PROPOSED DESIGN ONLY):**  
> `scripts/bootstrap-secrets.sh` is an architecture design specification to be implemented during Task 5 / Task 6. It has **not yet been created** in the codebase.

#### Bootstrap Requirements:
1. **High-Entropy Generation:** Use `/dev/urandom` via `openssl rand` or standard cryptographic randomness.
2. **Strict Operator Input Validation:** All external input parameters (`DOMAIN`, `ACME_EMAIL`) must be strictly validated before interpolation. Any input containing newline characters (`\n`, `\r`), semicolons, unescaped quotes, or failing standard FQDN and RFC-5322 email syntax validation causes immediate fail-closed termination. No unvalidated strings are passed to here-documents or environment files. Insecure default placeholder domains must require explicit operator confirmation or cause preflight failure.
3. **File Permissions & Umask:** Created files must be restricted strictly to file mode `0600` (`umask 077` and `chmod 0600`) owned by the operating user.
4. **Atomic No-Clobber & Symlink Defenses:**
   - To eliminate Time-of-Check to Time-of-Use (TOCTOU) races between file existence checks and writes, creating `.env` must use kernel-enforced atomic no-replace primitives (such as writing to a temporary private file and using `link` or `renameat2(..., RENAME_NOREPLACE)` via a small helper, or atomic no-clobber hard link creation `ln "$TMP_FILE" "$ENV_FILE"` before cleanup).
   - If `.env` is a symbolic link or exists without `--force`, operation must abort fail-closed immediately without following links.
   - When `--force` is specified, the overwrite must be performed via atomic rename (`mv -f "$TMP_FILE" "$ENV_FILE"`).
5. **Zero Secret Printing:** Secrets must **never** be echoed, printed to stdout, logged to stderr, or written to world-readable temporary paths.
6. **Masking & Validation:** Validate generated values prior to finalizing the write.

#### Proposed Bootstrap Implementation Contract:
```bash
#!/usr/bin/env bash
set -euo pipefail
umask 077

ENV_FILE=".env"
FORCE=0

for arg in "$@"; do
  case "$arg" in
    --force) FORCE=1 ;;
    *) echo "Unknown option: $arg" >&2; exit 1 ;;
  esac
done

# Require and strictly validate DOMAIN and ACME_EMAIL to prevent newline injection
DOMAIN="${DOMAIN:-}"
ACME_EMAIL="${ACME_EMAIL:-}"

if [ -z "$DOMAIN" ] || [ -z "$ACME_EMAIL" ]; then
  echo "ERROR: DOMAIN and ACME_EMAIL must be explicitly set in the environment." >&2
  exit 1
fi

# Reject newlines or carriage returns in inputs
if [[ "$DOMAIN" =~ [$'\r\n'] ]] || [[ "$ACME_EMAIL" =~ [$'\r\n'] ]]; then
  echo "ERROR: Control or newline characters detected in input parameters." >&2
  exit 1
fi

# Validate FQDN syntax (simple RFC-compliant pattern)
if ! [[ "$DOMAIN" =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)+$ ]]; then
  echo "ERROR: Invalid DOMAIN format. Must be a valid fully-qualified domain name." >&2
  exit 1
fi

# Validate Email syntax
if ! [[ "$ACME_EMAIL" =~ ^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$ ]]; then
  echo "ERROR: Invalid ACME_EMAIL format. Must be a valid RFC-5322 email address." >&2
  exit 1
fi

# Fail closed if target path is a symlink
if [ -L "$ENV_FILE" ]; then
  echo "ERROR: $ENV_FILE is a symbolic link. Aborting for security." >&2
  exit 1
fi

# Validate mandatory template existence fail-closed before generating any secrets
if [ ! -f "deploy/dex/config.prod.yml.template" ]; then
  echo "ERROR: Missing required template: deploy/dex/config.prod.yml.template" >&2
  exit 1
fi

if [ ! -f "deploy/redis/redis.conf.template" ]; then
  echo "ERROR: Missing required template: deploy/redis/redis.conf.template" >&2
  exit 1
fi

if [ ! -f "deploy/redis/redis-healthcheck.sh" ]; then
  echo "ERROR: Missing required healthcheck script: deploy/redis/redis-healthcheck.sh" >&2
  exit 1
fi

# Generate high-entropy secrets into private shell variables first
POSTGRES_DB="agent_tasks"
POSTGRES_USER="agent_user"
POSTGRES_PASSWORD=$(openssl rand -hex 24)
REDIS_PASSWORD=$(openssl rand -hex 24)
SECRET_ENCRYPTION_KEY=$(openssl rand -hex 32)
SESSION_SECRET=$(openssl rand -base64 32)
OIDC_CLIENT_ID="agent-control-plane"
OIDC_CLIENT_SECRET=$(openssl rand -hex 24)

# Setup staging directory for atomic generation and rollback
STAGE_DIR=$(mktemp -d .bootstrap.stage.XXXXXXXXXX)
chmod 0700 "$STAGE_DIR"

cleanup() {
  rm -rf "$STAGE_DIR"
}
trap cleanup EXIT

TMP_ENV="$STAGE_DIR/.env"
DEX_TMP="$STAGE_DIR/config.prod.yml"
REDIS_TMP="$STAGE_DIR/redis.conf"

# 1. Assemble staged .env content
cat <<EOF > "$TMP_ENV"
# Agent Task Manager - Production Environment Configuration
# Generated on: $(date -u +"%Y-%m-%dT%H:%M:%SZ")

DOMAIN=${DOMAIN}
ACME_EMAIL=${ACME_EMAIL}

POSTGRES_DB=${POSTGRES_DB}
POSTGRES_USER=${POSTGRES_USER}
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}

REDIS_PASSWORD=${REDIS_PASSWORD}
SECRET_ENCRYPTION_KEY=${SECRET_ENCRYPTION_KEY}

SESSION_SECRET=${SESSION_SECRET}
OIDC_CLIENT_ID=${OIDC_CLIENT_ID}
OIDC_CLIENT_SECRET=${OIDC_CLIENT_SECRET}

GIT_SSL_NO_VERIFY=false
APP_ENV=production
EOF
chmod 0600 "$TMP_ENV"

# 2. Render dependent service configurations using the exact same generated secrets
export DOMAIN OIDC_CLIENT_SECRET REDIS_PASSWORD
envsubst '${DOMAIN} ${OIDC_CLIENT_SECRET}' < deploy/dex/config.prod.yml.template > "$DEX_TMP"
# Dex runs as UID 1001: restricted mode 0640 (requires operator group 1001 or 0750 dir)
chmod 0640 "$DEX_TMP"

envsubst '${REDIS_PASSWORD}' < deploy/redis/redis.conf.template > "$REDIS_TMP"
# Redis runs as UID 999: restricted mode 0640 (requires operator group 999 or 0750 dir)
chmod 0640 "$REDIS_TMP"

chmod 0755 "deploy/redis/redis-healthcheck.sh"

# 3. Preflight check: reject symlinks in targets and all ancestor directories
TARGET_FILES=("$ENV_FILE" "deploy/dex/config.prod.yml" "deploy/redis/redis.conf")
PARENT_DIRS=("./" "deploy" "deploy/dex" "deploy/redis")

# Fail closed if any target or parent directory component is a symbolic link
for pdir in "${PARENT_DIRS[@]}"; do
  if [ -L "$pdir" ]; then
    echo "ERROR: Ancestor directory $pdir is a symbolic link. Aborting for security." >&2
    exit 1
  fi
done

for target in "${TARGET_FILES[@]}"; do
  if [ -L "$target" ]; then
    echo "ERROR: Target path $target is a symbolic link. Aborting for security." >&2
    exit 1
  fi
done

if [ "$FORCE" -ne 1 ]; then
  for target in "${TARGET_FILES[@]}"; do
    if [ -e "$target" ]; then
      echo "ERROR: Target file $target already exists. Aborting without modifying existing configurations. Use --force to overwrite." >&2
      exit 1
    fi
  done
fi

# 4. Operator ownership and directory permission verification:
# Dex runs as UID 1001, Redis runs as UID 999.
OPERATOR_UID=$(id -u)
mkdir -p deploy/redis deploy/dex
chmod 0750 deploy/dex deploy/redis

# Enforce operator ownership and non-writable permissions on parent directory components (POSIX mode bits)
for pdir in "./" "deploy" "deploy/dex" "deploy/redis"; do
  powner=$(stat -c '%u' "$pdir")
  if [ "$powner" -ne "$OPERATOR_UID" ] && [ "$powner" -ne 0 ]; then
    echo "ERROR: Ancestor directory $pdir is owned by untrusted UID $powner (expected $OPERATOR_UID or 0). Aborting." >&2
    exit 1
  fi
  # Verify directory has no group-write (0020) or other-write (0002) permissions
  pmode=$(stat -c '%a' "$pdir")
  if [ "$(( 0$pmode & 0022 ))" -ne 0 ]; then
    echo "ERROR: Ancestor directory $pdir is group-writable or world-writable (mode $pmode). Aborting for security." >&2
    exit 1
  fi
done

# Attempt group assignment on staging templates and parent service directories
if [ "$OPERATOR_UID" -eq 0 ]; then
  chown 0:1001 deploy/dex "$DEX_TMP"
  chown 0:999 deploy/redis "$REDIS_TMP"
else
  # Unprivileged operator must possess effective group membership in GID 1001 and 999
  if ! chgrp 1001 "$DEX_TMP" deploy/dex 2>/dev/null; then
    echo "ERROR: Host permission prerequisite failed: cannot assign GID 1001 to Dex configuration or deploy/dex directory." >&2
    echo "Operator prerequisite: add user to GID 1001/999 or run privileged setup: sudo chgrp 1001 deploy/dex && sudo chgrp 999 deploy/redis" >&2
    exit 1
  fi
  if ! chgrp 999 "$REDIS_TMP" deploy/redis 2>/dev/null; then
    echo "ERROR: Host permission prerequisite failed: cannot assign GID 999 to Redis configuration or deploy/redis directory." >&2
    echo "Operator prerequisite: add user to GID 1001/999 or run privileged setup: sudo chgrp 1001 deploy/dex && sudo chgrp 999 deploy/redis" >&2
    exit 1
  fi
fi

# Confirm group search and read permissions fail-closed for both directories and configuration files
if [ "$(stat -c '%g' deploy/dex)" != "1001" ] || [ "$(stat -c '%a' deploy/dex)" != "750" ]; then
  echo "ERROR: Dex directory permission verification failed (expected GID 1001, mode 0750 on deploy/dex)." >&2
  exit 1
fi
if [ "$(stat -c '%g' "$DEX_TMP")" != "1001" ] || [ "$(stat -c '%a' "$DEX_TMP")" != "640" ]; then
  echo "ERROR: Dex configuration permission verification failed (expected GID 1001, mode 0640)." >&2
  exit 1
fi

if [ "$(stat -c '%g' deploy/redis)" != "999" ] || [ "$(stat -c '%a' deploy/redis)" != "750" ]; then
  echo "ERROR: Redis directory permission verification failed (expected GID 999, mode 0750 on deploy/redis)." >&2
  exit 1
fi
if [ "$(stat -c '%g' "$REDIS_TMP")" != "999" ] || [ "$(stat -c '%a' "$REDIS_TMP")" != "640" ]; then
  echo "ERROR: Redis configuration permission verification failed (expected GID 999, mode 0640)." >&2
  exit 1
fi

# 5. Staged publication with atomic no-replace / verified replace and rollback
# Notice on Multi-File Atomicity & TOCTOU Boundaries:
# Target directory trees (deploy/dex, deploy/redis) must be operator-owned without write access
# by untrusted principals, ensuring stable ancestor paths during publication.
# Individual file publishing uses kernel-enforced atomic no-replace (ln) or verified atomic replace (mv).
# Across power loss or uncatchable SIGKILL, multi-file publishing cannot be globally guaranteed
# without filesystem-level generation directories or activation pointers.
# To achieve practical recoverability, all preexisting targets are backed up first.
# If any publish step fails, all temporary files and newly created targets are removed,
# and all backups are restored before exiting.
BACKUP_DIR=$(mktemp -d .bootstrap.backup.XXXXXXXXXX)
chmod 0700 "$BACKUP_DIR"
rollback_list=()
newly_created=()
pending_dst_tmps=()

rollback_all() {
  echo "WARNING: Bootstrap publish failed. Rolling back all modified files..." >&2
  local cleanup_failed=0
  local restore_failed=0
  for dtmp in "${pending_dst_tmps[@]}"; do
    if [ -e "$dtmp" ] || [ -L "$dtmp" ]; then
      if ! rm -f "$dtmp" 2>/dev/null; then
        echo "WARNING: Failed to remove temporary secret file: $dtmp" >&2
        cleanup_failed=1
      fi
    fi
  done
  for created in "${newly_created[@]}"; do
    if [ -e "$created" ] || [ -L "$created" ]; then
      if ! rm -f "$created" 2>/dev/null; then
        echo "WARNING: Failed to remove newly created target: $created" >&2
        cleanup_failed=1
      fi
    fi
  done
  for entry in "${rollback_list[@]}"; do
    orig="${entry%%:*}"
    bak="${entry##*:}"
    if [ -f "$bak" ]; then
      if ! mv -f "$bak" "$orig" 2>/dev/null; then
        echo "CRITICAL ERROR: Failed to restore original file $orig from backup $bak!" >&2
        restore_failed=1
      fi
    fi
  done
  if [ "$restore_failed" -eq 1 ] || [ "$cleanup_failed" -eq 1 ]; then
    echo "MANUAL RECOVERY REQUIRED: Preexisting configuration backups preserved in $BACKUP_DIR" >&2
  else
    rm -rf "$BACKUP_DIR"
  fi
  rm -rf "$STAGE_DIR"
}

# Trap failure during publish
trap rollback_all ERR EXIT

# Backup all preexisting targets prior to moving any staged files
for target in "${TARGET_FILES[@]}"; do
  if [ -e "$target" ] || [ -L "$target" ]; then
    # Reject non-regular files (FIFOs, devices, sockets, directories) before attempting backup
    if [ ! -f "$target" ] && [ ! -L "$target" ]; then
      echo "ERROR: Target path $target exists but is not a regular file. Aborting." >&2
      exit 1
    fi
    bfile="$BACKUP_DIR/$(basename "$target").bak"
    cp -p "$target" "$bfile"
    rollback_list+=("$target:$bfile")
  fi
done

publish_file() {
  local src="$1"
  local dst="$2"
  local expected_mode="$3"
  local expected_gid="$4"
  local dst_dir
  dst_dir=$(dirname "$dst")

  # Verify parent directory is not a symlink, is operator-owned, and is not group/other-writable
  if [ -L "$dst_dir" ]; then
    echo "ERROR: Destination directory $dst_dir is a symbolic link. Aborting publication." >&2
    exit 1
  fi
  local powner
  powner=$(stat -c '%u' "$dst_dir")
  if [ "$powner" -ne "$OPERATOR_UID" ] && [ "$powner" -ne 0 ]; then
    echo "ERROR: Destination directory $dst_dir is owned by untrusted UID $powner. Aborting." >&2
    exit 1
  fi
  local pmode
  pmode=$(stat -c '%a' "$dst_dir")
  if [ "$(( 0$pmode & 0022 ))" -ne 0 ]; then
    echo "ERROR: Destination directory $dst_dir is group- or other-writable (mode $pmode). Aborting." >&2
    exit 1
  fi

  # Create destination temporary file in the target directory (guarantees same-filesystem atomic rename)
  local dst_tmp
  dst_tmp=$(mktemp "$dst_dir/.$(basename "$dst").tmp.XXXXXXXXXX")
  pending_dst_tmps+=("$dst_tmp")
  cp -p "$src" "$dst_tmp"

  # Validate dst_tmp is a regular file and strictly retained required mode and group
  if [ ! -f "$dst_tmp" ] || [ -L "$dst_tmp" ]; then
    echo "ERROR: Staged temporary $dst_tmp is not a regular file. Aborting." >&2
    exit 1
  fi
  if [ "$(stat -c '%a' "$dst_tmp")" != "$expected_mode" ]; then
    echo "ERROR: Staged temporary $dst_tmp mode mismatch (got $(stat -c '%a' "$dst_tmp"), expected $expected_mode). Aborting." >&2
    exit 1
  fi
  if [ -n "$expected_gid" ] && [ "$(stat -c '%g' "$dst_tmp")" != "$expected_gid" ]; then
    echo "ERROR: Staged temporary $dst_tmp GID mismatch (got $(stat -c '%g' "$dst_tmp"), expected $expected_gid). Aborting." >&2
    exit 1
  fi

  if [ "$FORCE" -ne 1 ]; then
    # Atomic kernel-enforced no-replace via hard link creation:
    # If $dst already exists (or raced into existence), ln fails atomically without replacing or following symlinks.
    if ! ln "$dst_tmp" "$dst" 2>/dev/null; then
      echo "ERROR: Atomic no-replace failed: destination $dst already exists or link failed." >&2
      exit 1
    fi
    # Register successfully linked destination before fallible cleanup
    newly_created+=("$dst")
    rm -f "$dst_tmp"
  else
    # In forced mode, reject destination if it is a symlink or non-regular file to prevent following links or clobbering devices
    if [ -L "$dst" ]; then
      echo "ERROR: Forced replacement of symbolic link $dst is prohibited." >&2
      exit 1
    fi
    if [ -e "$dst" ] && [ ! -f "$dst" ]; then
      echo "ERROR: Forced replacement of non-regular file $dst is prohibited." >&2
      exit 1
    fi
    if [ ! -e "$dst" ]; then
      newly_created+=("$dst")
    fi
    mv -f "$dst_tmp" "$dst"
  fi

  # Post-publication metadata verification on final target path
  if [ "$(stat -c '%a' "$dst")" != "$expected_mode" ]; then
    echo "ERROR: Published file $dst mode verification failed (got $(stat -c '%a' "$dst"), expected $expected_mode)." >&2
    exit 1
  fi
  if [ -n "$expected_gid" ] && [ "$(stat -c '%g' "$dst")" != "$expected_gid" ]; then
    echo "ERROR: Published file $dst GID verification failed (got $(stat -c '%g' "$dst"), expected $expected_gid)." >&2
    exit 1
  fi
}

publish_file "$TMP_ENV" "$ENV_FILE" "600" ""
publish_file "$DEX_TMP" "deploy/dex/config.prod.yml" "640" "1001"
publish_file "$REDIS_TMP" "deploy/redis/redis.conf" "640" "999"

# Publish succeeded completely: disarm trap and clean up backups
trap - ERR EXIT
rm -rf "$BACKUP_DIR" "$STAGE_DIR"
echo "SUCCESS: Staged and published production .env (mode 0600) and service configurations (mode 0640 in 0750 directories)."
```

---

## 5. `SECRET_ENCRYPTION_KEY` Representation & Migration Contract

### 5.1 Analysis of Alternatives

The AES-256-GCM cipher implemented in `internal/crypto` requires a 256-bit (32-byte) key. Today, `internal/config/config.go:68` loads `SECRET_ENCRYPTION_KEY` as a string, and `cmd/server/main.go:114-117` converts it:
```go
masterKey := []byte(cfg.SecretEncryptionKey)
if len(masterKey) != 32 {
    log.Fatalf("SECRET_ENCRYPTION_KEY must be exactly 32 bytes (got %d)", len(masterKey))
}
```

Three representation alternatives were evaluated:

| Alternative | Description | Pros | Cons / Compatibility | Recommendation |
|---|---|---|---|---|
| **Option 1: Retain Literal 32-Byte String** | Require exactly 32 ASCII characters. | 100% backwards compatible with existing code and tests. | Difficult to represent arbitrary binary keys in `.env` without escaping or non-printable character issues. | Acceptable interim baseline. |
| **Option 2: Strict Hex-Encoded 64-Hex String Only** | Require a 64-character hexadecimal string (`[0-9a-fA-F]{64}`). | Standard representation for 256-bit cryptographic keys; shell and `.env` safe. | **Breaks existing installations** using literal 32-byte keys unless an explicit migration path is executed. | Incompatible without migration. |
| **Option 3: Typed Dual-Format Decoding (PROPOSED TARGET)** | Inspect string format: if exactly 64 valid hex characters, decode via `hex.DecodeString`; else if length is exactly 32 bytes, treat as raw ASCII bytes; otherwise reject. | Clean migration path; allows new deployments to use standard 64-hex strings while preserving decryptability for legacy literal 32-byte installations. | Requires explicit decoding logic and unambiguous format precedence. | **RECOMMENDED PROPOSAL** |

### 5.2 Proposed Config Contract & Non-Secret Error Messages

#### Validation Rules:
1. Normalize and decode key input:
   - If string length is 64 and matches `^[0-9a-fA-F]{64}$`, decode as hexadecimal into 32 raw bytes.
   - Else if string length is 32, convert literal ASCII string to 32 raw bytes (legacy compatibility).
   - If neither condition is met, fail closed with explicit non-secret length/format errors.
2. Canonical weak key rejection (post-normalization):
   After decoding to 32 raw bytes, compare the resulting byte sequence (or its canonical 64-hex string) against canonical known test keys:
   - Literal ASCII `"01234567890123456789012345678901"` (hex: `3031323334353637383930313233343536373839303132333435363738393031`)
   - Literal ASCII `"12345678901234567890123456789012"` (hex: `3132333435363738393031323334353637383930313233343536373839303132`)
   - 32 repeated zero bytes (`0x00` repeated 32 times, derived from 64 hex `'0'` characters: `0000...0000`) or 32 literal ASCII `'0'` characters (`0x30` repeated 32 times: `3030...3030`)
   - 32 repeated `0x11` bytes (derived from 64 hex `'1'` characters: `1111...1111`) or 32 literal ASCII `'1'` characters (`0x31` repeated 32 times: `3131...3131`)
   If the decoded key matches any known test pattern, fail closed immediately. Notice: The check rejects these specific canonical test and default patterns. It does not perform subjective runtime entropy scoring.

#### Explicit Non-Secret Error Messages:
- Empty key:  
  `"SECRET_ENCRYPTION_KEY is required and cannot be empty"`
- Known default/example detected:  
  `"SECRET_ENCRYPTION_KEY contains an insecure default or example value; generate a production key"`
- Invalid length/encoding:  
  `"invalid SECRET_ENCRYPTION_KEY: must be either a 64-character hex string or exactly 32 bytes literal (got length %d)"`
- Hex decode failure (e.g. 64 chars with non-hex characters):  
  `"invalid SECRET_ENCRYPTION_KEY: 64-character key contains invalid hex encoding"`

> **CRITICAL LOGGING INVARIANT:**  
> Error messages, logs, and preflight outputs must **never print the secret value**, key slices, or raw characters. Only key length and format classification may be included in diagnostic output.

### 5.3 Backup, Restore & Key Rotation Procedure

Because existing database rows (`models.Credential`, `models.ProjectIntegration`) store credentials encrypted via AES-256-GCM using this key:
1. **Key Replacement Without Re-encryption Breaks Data:** Changing `SECRET_ENCRYPTION_KEY` in the environment without re-encrypting existing database rows renders all stored credentials permanently unreadable (GCM authentication failure).
2. **Key Rotation CLI Requirement (Future Task):** A dedicated rotation CLI command must be developed before key rotation can be executed. Crucially, secret keys must **never be accepted via command-line arguments (`argv`)**, as command lines are visible in `/proc/<pid>/cmdline`, process listings (`ps aux`), and shell histories. Instead, the CLI must accept keys via file descriptors, temporary secret files, or standard input:
   ```bash
   # Safe rotation via secret file descriptors or input files:
   atmctl secrets rotate-encryption-key \
     --old-key-file /run/secrets/old_encryption_key \
     --new-key-file /run/secrets/new_encryption_key
   ```
   Alternatively, keys can be streamed interactively or via pipes (`cat key.txt | atmctl secrets rotate-encryption-key --from-stdin`).
   This tool will read each encrypted record using the old key, decrypt the plaintext in memory, re-encrypt with the new key, and commit within an atomic database transaction.
3. **Backup Mandate:** Before rotating or altering the encryption key, a full PostgreSQL database dump (`pg_dump`) and `.env` backup must be performed.

---

## 6. Startup Fail-Closed Validation & Preflight Checks

### 6.1 Control Plane Startup Validation

The control plane configuration loader (`internal/config/config.go` or startup preflight in `cmd/server/main.go`) must enforce fail-closed validation when `APP_ENV=production` or `PREFLIGHT_STRICT=true`.

#### Banned Default Secret List:
Startup must fail if any of the following known test secrets are detected in production:

| Variable | Rejected Insecure Values |
|---|---|
| `POSTGRES_PASSWORD` | `"postgres"`, `""`, `"password"`, `"admin"`, `"root"`, `"secret"` |
| `REDIS_PASSWORD` | `"secretredis"`, `""`, `"redis"`, `"password"`, `"admin"` |
| `SECRET_ENCRYPTION_KEY` | `"01234567890123456789012345678901"`, `"00000000000000000000000000000000"`, `"11111111111111111111111111111111"`, `"12345678901234567890123456789012"` |
| `SESSION_SECRET` | `"dev-session-secret-change-in-prod-1234"`, `""`, `"secret"`, `"session"` |
| `OIDC_CLIENT_SECRET` | `"dev-oidc-secret-change-in-prod-1234"`, `""`, `"secret"` |
| `OPENCODE_PASSWORD` | `"4YXQ3udRl8k_UkFdyhHSFF-bdInQgKk0F9Qz6zp2y0U"`, `""`, `"opencode"` (evaluated conditionally if legacy transport authentication is active; long-term daemon password lifecycle is governed by Gate 0G) |

#### Proposed Validation & Normalization Pipeline:

To eliminate type and contract mismatches between existing environment strings and the proposed configuration structs, validation is performed in a two-stage pipeline:
1. `ValidateRawProductionConfig(raw *RawEnvironmentConfig) error`: Validates unmarshaled environment strings prior to decoding or cryptographic usage. It safely parses and checks individual credentials (extracting the database password directly or from database connection strings).
2. `NormalizeToProposedConfig(raw *RawEnvironmentConfig) (*ProposedConfig, error)`: Normalizes validated raw strings into typed structures (e.g. converting `SecretEncryptionKey` to `MasterEncryptionKey{[32]byte, isHex}`) for runtime use.

```go
package config

import (
    "encoding/hex"
    "errors"
    "fmt"
    "net/url"
    "os"
)

// RawEnvironmentConfig captures the initial unmarshaled environment inputs.
type RawEnvironmentConfig struct {
    AppEnv              string
    PreflightStrict     bool
    DatabaseURL         string
    DatabasePassword    string // Parsed from DatabaseURL or environment
    RedisPassword       string
    SecretEncryptionKey string
    SessionSecret       string
    OIDCClientSecret    string
    OpenCodeAuthMode    string // "legacy_static" or "gate_0g_dynamic"
    OpenCodePassword    string
}

// ValidateRawProductionConfig performs deterministic fail-closed validation on raw inputs.
func ValidateRawProductionConfig(raw *RawEnvironmentConfig) error {
    isProduction := raw.AppEnv == "production"
    isPreflightStrict := raw.PreflightStrict || os.Getenv("PREFLIGHT_STRICT") == "true"
    if !isProduction && !isPreflightStrict {
        return nil
    }

    // Determine database password from direct field or DSN
    dbPass := raw.DatabasePassword
    if dbPass == "" && raw.DatabaseURL != "" {
        if u, err := url.Parse(raw.DatabaseURL); err == nil && u.User != nil {
            dbPass, _ = u.User.Password()
        }
    }

    insecurePostgres := map[string]bool{
        "postgres": true,
        "password": true,
        "admin":    true,
        "root":     true,
        "secret":   true,
        "":         true,
    }
    if insecurePostgres[dbPass] {
        return errors.New("production preflight check failed: database password uses an insecure default or is empty")
    }

    insecureRedis := map[string]bool{
        "secretredis": true,
        "redis":       true,
        "password":    true,
        "admin":       true,
        "":            true,
    }
    if insecureRedis[raw.RedisPassword] {
        return errors.New("production preflight check failed: REDIS_PASSWORD uses an insecure default or is empty")
    }

    // Encryption key normalization and weak key check
    var rawKey []byte
    keyStr := raw.SecretEncryptionKey
    if keyStr == "" {
        return errors.New("SECRET_ENCRYPTION_KEY is required and cannot be empty")
    }
    if len(keyStr) == 64 {
        decoded, err := hex.DecodeString(keyStr)
        if err != nil {
            return errors.New("invalid SECRET_ENCRYPTION_KEY: 64-character key contains invalid hex encoding")
        }
        rawKey = decoded
    } else if len(keyStr) == 32 {
        rawKey = []byte(keyStr)
    } else {
        return fmt.Errorf("invalid SECRET_ENCRYPTION_KEY: must be either a 64-character hex string or exactly 32 bytes literal (got length %d)", len(keyStr))
    }

    // Compare canonical normalized 32-byte representation against known weak test keys
    insecureHexKeys := map[string]bool{
        "3031323334353637383930313233343536373839303132333435363738393031": true, // "01234567890123456789012345678901" (literal ASCII)
        "3132333435363738393031323334353637383930313233343536373839303132": true, // "12345678901234567890123456789012" (literal ASCII)
        "3030303030303030303030303030303030303030303030303030303030303030": true, // 32 '0' ASCII chars
        "3131313131313131313131313131313131313131313131313131313131313131": true, // 32 '1' ASCII chars
        "0000000000000000000000000000000000000000000000000000000000000000": true, // 32 zero bytes (from 64 hex '0's)
        "1111111111111111111111111111111111111111111111111111111111111111": true, // 32 0x11 bytes (from 64 hex '1's)
    }
    if insecureHexKeys[hex.EncodeToString(rawKey)] {
        return errors.New("production preflight check failed: SECRET_ENCRYPTION_KEY uses an insecure default")
    }

    if raw.SessionSecret == "dev-session-secret-change-in-prod-1234" || len(raw.SessionSecret) < 32 {
        return errors.New("production preflight check failed: SESSION_SECRET uses an insecure default or is shorter than 32 characters")
    }

    insecureOIDC := map[string]bool{
        "dev-oidc-secret-change-in-prod-1234": true,
        "secret":                              true,
        "":                                    true,
    }
    if insecureOIDC[raw.OIDCClientSecret] {
        return errors.New("production preflight check failed: OIDC_CLIENT_SECRET uses an insecure default or is empty")
    }

    // OpenCode password lifecycle is governed by Gate 0G (remains unresolved).
    // Preflight check validates legacy static password ONLY when legacy static mode is active:
    if raw.OpenCodeAuthMode == "legacy_static" {
        insecureOpenCode := map[string]bool{
            "4YXQ3udRl8k_UkFdyhHSFF-bdInQgKk0F9Qz6zp2y0U": true,
            "opencode":                                   true,
            "":                                           true,
        }
        if insecureOpenCode[raw.OpenCodePassword] {
            return errors.New("production preflight check failed: OPENCODE_PASSWORD uses an insecure default or is empty in legacy static auth mode")
        }
    }

    return nil
}
```

#### Proposed Future Configuration Schema (`internal/config/config.go`)

> **DESIGN NOTICE (NOT YET IMPLEMENTED):**  
> The existing application configuration struct (`internal/config/config.go:12-56`) loads secrets as literal environment strings. To support strict preflight validation, file-backed secret mounting, and dual-format encryption key decoding, the following configuration struct and secret descriptor interfaces are proposed for downstream implementation:

```go
package config

// SecretSource represents the origin and status of a loaded credential.
type SecretSource string

const (
    SourceEnvFile   SecretSource = "env_file"
    SourceFileMount SecretSource = "file_mount"
    SourceRawEnv    SecretSource = "raw_env"
)

// MasterEncryptionKey holds a validated and normalized 256-bit AES master key.
type MasterEncryptionKey struct {
    RawBytes    [32]byte
    IsHexFormat bool
}

// ProductionHardeningConfig governs preflight boundaries and secret validation.
type ProductionHardeningConfig struct {
    PreflightStrict  bool
    RequireTLS       bool
    DisallowDefaults bool
}

// ProposedConfig represents the modernized target application configuration contract.
type ProposedConfig struct {
    AppEnv    string
    Port      int
    Hardening ProductionHardeningConfig

    // Secrets (Validated and Normalized)
    MasterKey     MasterEncryptionKey
    SessionSecret string

    // Database & Cache Credentials
    DatabaseURL      string
    DatabasePassword string // Explicitly retained in memory for direct sub-service dials
    RedisAddr        string
    RedisPassword    string

    // Ingress & Identity
    Domain           string
    OIDCClientID     string
    OIDCClientSecret string

    // Unresolved Lifecycles (Interfaces defined by Gate 0G)
    OpenCodeAuthMode string
    OpenCodePassword string // Evaluated only when OpenCodeAuthMode == "legacy_static"
}
```

---

## 7. Open Architecture Decision Gates

### 7.1 Decision Gate: Repository `LICENSE` Resolution

- **Resolution Status:** **APPROVED AT CHECKPOINT A (2026-10-13)**.
- **Decision:** The repository maintainer and project owner formally approved the **MIT License**.
- **Implementation Action:** Downstream Task 1 in `docs/superpowers/plans/2026-10-11-deployment-and-hardening.md` will create the tracked `LICENSE` file containing standard MIT license terms, aligning with the existing README badge.

### 7.2 Decision Gate: Runtime Catalog Version Approval & Source Status Reconciliation

- **Resolution Status:** **APPROVED AT CHECKPOINT A (2026-10-13)**.
- **Decision:** User formally approved the exact pinned baseline catalog versions across all specifications:
  - Go: `1.27.2`
  - Node.js: `24.21.0`
  - Python: `3.11.17`
  - Java: `25.0.4.1+1`
  - Rust: `1.99.0`
  - .NET: `10.0.401`
  - OpenCode GHCR Pin: `ghcr.io/anomalyco/opencode:2.0.26` (OCI manifest candidate: `sha256:ea6c5d68d8d230923d2627bc793e7fa6a03f8e47c253b50397eb92bbde6db255`)
- **Implementation Action:** Codified into `internal/runner/runtime_catalog.go` during runtime implementation plan execution.

### 7.3 Decision Gate: Dex OIDC Storage Baseline

- **Resolution Status:** **APPROVED AT CHECKPOINT A (2026-10-13)**.
- **Decision:** Dex `storage: type: memory` is formally approved for the initial v1 production deployment baseline. Persistent PostgreSQL storage remains a documented opt-in migration for multi-instance or high-availability deployments.

---

## 8. Qualification Probes Specification

The following qualification probes establish objective pass/fail criteria for Gate 0F.

> **SPECIFICATION NOTICE (FUTURE QUALIFICATION PROBES):**  
> These probes are design requirements governing future verification during Task 5 / Task 6 implementation. They have **not yet been executed**, as implementation of `scripts/bootstrap-secrets.sh` and production hardening code has not begun. All probes must be executed on a clean staging host prior to production sign-off.

### Probe 0F-1: Traefik Docker Socket Decoupling Probe
- **Objective:** Verify that Traefik has no access to the Docker daemon socket and routes traffic strictly via file-based configuration.
- **Execution:**
  1. Inspect Traefik container mounts:
     `docker inspect agent-traefik --format '{{ range .Mounts }}{{ .Source }} -> {{ .Destination }}{{ "\n" }}{{ end }}'`
  2. Attempt to query Docker daemon from inside Traefik container:
     `docker exec agent-traefik ls -l /var/run/docker.sock`
- **Pass Criteria:**
  - Mount inspection contains zero references to `/var/run/docker.sock`.
  - Inside the container, `/var/run/docker.sock` does not exist (exit status non-zero: `No such file or directory`).
  - Web UI, Control Plane API (`/api/v1`), and Dex endpoints respond successfully through Traefik ingress over ports 80/443.

### Probe 0F-2: Default Secret Rejection Probe (Negative Test)
- **Objective:** Confirm that the control plane immediately fails to start if insecure default secrets are supplied in production or strict preflight mode.
- **Execution:**
  1. Set `APP_ENV=production` and `SECRET_ENCRYPTION_KEY=01234567890123456789012345678901`.
  2. Execute control plane binary or container startup: `go run cmd/server/main.go` or `docker compose up control-plane`.
- **Pass Criteria:**
  - Process exits immediately with non-zero exit code (`1`).
  - Stderr contains explicit failure message: `"production preflight check failed: SECRET_ENCRYPTION_KEY uses an insecure default"`.
  - Process does not print the secret value to stderr or stdout.

### Probe 0F-3: Bootstrap Script Atomic Creation, Race, Symlink Defense, and Rollback Probe
- **Objective:** Verify that `scripts/bootstrap-secrets.sh` safely creates a `.env` file with mode `0600`, enforces non-root container permissions (`0640` in `0750` directories with required service GIDs), defends against symlinks and race conditions, executes atomic no-replace, and rolls back cleanly on publication failures.
- **Execution (Future Verification Contract):**
  1. **Clean Run Test:** Run `./scripts/bootstrap-secrets.sh` in an isolated temporary directory with valid templates. Inspect permissions: `stat -c "%a" .env`. Verify mode is `600`. Verify `deploy/dex/config.prod.yml` and `deploy/redis/redis.conf` have mode `640` within `0750` directories.
  2. **No-Clobber Duplicate Run Test:** Run `./scripts/bootstrap-secrets.sh` a second time without `--force`. Confirm exit code `1` and error message containing `"Target file ... already exists"`.
  3. **Symlink and Ancestor Symlink & Writable Parent Defense Test:**
     - Create a direct symlink `.env -> /tmp/target_canary`. Confirm immediate abort with exit code `1`.
     - Create an ancestor symlink `deploy/dex -> /tmp/dex_target`. Confirm immediate abort with exit code `1` (`"ERROR: Ancestor directory ... is a symbolic link"`).
     - Test dangling symlink: create dangling link `deploy/redis/redis.conf -> /nonexistent`. Confirm fail-closed rejection without following.
     - Test group-writable ancestor directory: set `chmod 0770 deploy`. Run `./scripts/bootstrap-secrets.sh`. Confirm immediate fail-closed termination (`"ERROR: Ancestor directory deploy is group-writable or world-writable"`).
  4. **Injected Failure, Link Recovery, and Rollback Test:**
     - Simulate an unlink failure of the temporary file immediately after `ln` succeeds during `publish_file`. Verify that `dst` is registered in `newly_created` and cleanly rolled back.
     - Simulate a failure during the second publish step (e.g. read-only filesystem or permissions failure on `deploy/dex/config.prod.yml`).
     - Confirm that `publish_file` initiates `rollback_all`, removes any newly created files, restores all preexisting file backups, and cleans up temporary secret files.
     - Simulate a permission failure during cleanup of a secret temp file (`pending_dst_tmps`). Confirm that `cleanup_failed` is flagged, an explicit warning is emitted without leaking secrets, and backups in `.bootstrap.backup.*` are preserved for manual recovery.
     - Verify that if rollback itself encounters an unrecoverable disk error, preexisting backups in `.bootstrap.backup.*` are preserved for manual recovery and logged loudly.
  5. **Redis Healthcheck Fail-Closed Authentication Test:**
     - Test valid configuration: with exactly 1 non-empty `requirepass` matching Redis runtime, verify `redis-healthcheck.sh` exits `0`.
     - Test missing directive: remove `requirepass` from `redis.conf`; verify `redis-healthcheck.sh` exits `1` with explicit error even if Redis is running and accepting unauthenticated connections.
     - Test empty password: configure `requirepass ""` or whitespace; verify `redis-healthcheck.sh` exits `1`.
     - Test duplicate directive: configure multiple `requirepass` lines; verify `redis-healthcheck.sh` detects `COUNT != 1` and exits `1`.
     - Verify zero secret passwords appear in stdout or stderr during healthcheck execution.
  6. **Force Overwrite and Non-Regular Target Test:**
     - Create an invalid pre-existing target (e.g. directory or FIFO at `deploy/redis/redis.conf`). Run `./scripts/bootstrap-secrets.sh --force`. Confirm fail-closed rejection (`"ERROR: Forced replacement of non-regular file ... is prohibited"`).
     - Run `./scripts/bootstrap-secrets.sh --force` against valid regular files. Confirm exit code `0` and atomic file replacement with mode `600`/`640`.
- **Pass Criteria:**
  - Clean run produces `.env` with exact octal mode `600` and service configs with mode `640`.
  - Pre-existing files without `--force` are never replaced or corrupted.
  - Symlinks (direct, ancestor, or dangling) and non-regular objects are never followed or clobbered.
  - Failures trigger clean rollback without leaving stray secret-bearing temporary files.
  - Zero secret values appear in terminal stdout/stderr.

### Probe 0F-4: Secret Encryption Key Format Validation Probe
- **Objective:** Validate that the encryption key loader accepts both 64-character hex strings and legacy 32-byte literals, while rejecting invalid lengths and formats without leaking values.
- **Execution:**
  1. Test with 64-character hex key: `SECRET_ENCRYPTION_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef` -> verify successful key decoding.
  2. Test with 32-byte high-entropy literal -> verify successful loading.
  3. Test with 63-character string -> verify failure with error containing `"invalid SECRET_ENCRYPTION_KEY: must be either a 64-character hex string or exactly 32 bytes literal (got length 63)"`.
  4. Test with 64-character non-hex string (e.g. ending in `zzzz`) -> verify failure with error containing `"invalid hex encoding"`.
- **Pass Criteria:** Valid keys decode to exactly 32 raw bytes; invalid inputs fail closed with sanitized error messages.

---

## 9. Architectural Dependencies & Scope Boundaries

### 9.1 Relationship to Other Qualification Gates

```
┌────────────────────────────────────────────────────────┐
│ Gate 0A & 0B: Reconciled Runtime Verification Evidence │
└───────────────────────────┬────────────────────────────┘
                            │
                            ▼
┌────────────────────────────────────────────────────────┐
│ Gate 0C: Git Transfer  │ Gate 0D: Egress Confinement   │
│ (Bundle / Helper Pipe) │ (Hybrid DMZ Proxy / Allowlist)│
└───────────────────────────┬────────────────────────────┘
                            │
                            ▼
┌────────────────────────────────────────────────────────┐
│ Gate 0G: Model Credentials │ Gate 0F: Production Compose│
│ (Gateway Mediation)        │ (Traefik Decouple / Secs)  │
└───────────────────────────┬────────────────────────────┘
                            │
                            ▼
┌────────────────────────────────────────────────────────┐
│ Checkpoint A: Architectural Sign-off & Gate Freeze    │
└────────────────────────────────────────────────────────┘
```

1. **Gate 0D Egress Confinement:**
   Gate 0F defines the Compose topology for the base infrastructure (`traefik`, `postgres`, `redis`, `dex`, `control-plane`, `web-ui`). Gate 0D specifies the network egress rules and forward proxy DMZ for *sandbox* containers. The sandbox networks are dynamically orchestrated by the control plane and attach to the forward proxy specified in Gate 0D.
2. **Gate 0G Model Credential Isolation:**
   Gate 0F removes hardcoded `OPENCODE_PASSWORD` from Compose. The dynamic generation, injection, and same-container process isolation of OpenCode credentials are governed by Gate 0G.
3. **Gate 0E Codebase Decoupling (Downstream):**
   Gate 0F configures `GIT_SSL_NO_VERIFY: "false"`. Gate 0E (in Task 7) will eliminate the code-level `http.sslVerify=false` fallbacks and host execution routines (`runOnHost`) in `internal/runner/task_runner.go`.

### 9.2 Scope Boundaries

- **In Scope for Gate 0F:**
  - Production Compose topology specification.
  - Decoupling Traefik from `/var/run/docker.sock`.
  - Bootstrap script contract and secret inventory.
  - `SECRET_ENCRYPTION_KEY` representation rules and non-secret error handling.
  - Default secret preflight validation rules.
  - Recording the repository `LICENSE` and runtime catalog decision gates.
- **Out of Scope (Delegated to Other Tasks):**
  - Application code implementation (Task 7 / Gate 0E).
  - Runtime container Dockerfiles (Task 6 / downstream runtime plan).
  - Modifying host firewall (`nftables`) or Docker daemon configuration.
  - Modifying live `.env` files or committing changes.
