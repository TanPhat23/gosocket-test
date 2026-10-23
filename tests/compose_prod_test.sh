#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="${REPO_ROOT}/docker-compose.prod.yml"

echo "=== Testing Production Docker Compose Manifest ==="

# Existence check
if [[ ! -f "${COMPOSE_FILE}" ]]; then
    echo "ERROR: ${COMPOSE_FILE} not found" >&2
    exit 1
fi

python3 -c "
import yaml, sys

with open('${COMPOSE_FILE}', 'r') as f:
    try:
        data = yaml.safe_load(f)
    except Exception as e:
        print(f'YAML parse error in ${COMPOSE_FILE}: {e}', file=sys.stderr)
        sys.exit(1)

if not isinstance(data, dict):
    print('ERROR: Root element of docker-compose.prod.yml must be a mapping/dict', file=sys.stderr)
    sys.exit(1)

services = data.get('services', {})
networks = data.get('networks', {})
volumes = data.get('volumes', {})

print('PASS: YAML syntax valid.')

# Test 1: Traefik service has zero mounts matching docker.sock
traefik = services.get('traefik')
if not traefik:
    print('ERROR: traefik service is missing', file=sys.stderr)
    sys.exit(1)

traefik_vols = traefik.get('volumes', [])
for vol in traefik_vols:
    vol_str = str(vol)
    if 'docker.sock' in vol_str:
        print(f'ERROR: traefik service mounts docker.sock: {vol_str}', file=sys.stderr)
        sys.exit(1)

# Check traefik mounts deploy configs & traefik_acme
traefik_vols_str = [str(v) for v in traefik_vols]
has_traefik_yml = any('deploy/traefik/traefik.yml' in v for v in traefik_vols_str)
has_dynamic_yml = any('deploy/traefik/dynamic.yml' in v for v in traefik_vols_str)
has_acme_vol = any('traefik_acme:/letsencrypt' in v for v in traefik_vols_str)

if not (has_traefik_yml and has_dynamic_yml and has_acme_vol):
    print(f'ERROR: traefik volumes missing required mounts. Found: {traefik_vols_str}', file=sys.stderr)
    sys.exit(1)

print('PASS: Test 1 - Traefik service has zero docker.sock mounts and correct volume mounts.')

# Test 2: Networks include external agent_ingress and internal agent_backend
ingress_net = networks.get('ingress', {})
backend_net = networks.get('backend', {})

if ingress_net.get('name') != 'agent_ingress' or ingress_net.get('driver') != 'bridge':
    print(f'ERROR: ingress network misconfigured: {ingress_net}', file=sys.stderr)
    sys.exit(1)

if backend_net.get('name') != 'agent_backend' or backend_net.get('driver') != 'bridge' or backend_net.get('internal') is not True:
    print(f'ERROR: backend network misconfigured (must have name: agent_backend, driver: bridge, internal: True): {backend_net}', file=sys.stderr)
    sys.exit(1)

print('PASS: Test 2 - Networks include external agent_ingress and internal agent_backend.')

# Test 3: PostgreSQL 18 specifies PGDATA: /var/lib/postgresql/18/docker and volume postgres_data:/var/lib/postgresql
postgres = services.get('postgres')
if not postgres:
    print('ERROR: postgres service is missing', file=sys.stderr)
    sys.exit(1)

if 'postgres:18' not in str(postgres.get('image')):
    print(f'ERROR: postgres image must be postgres:18-alpine, got: {postgres.get(\"image\")}', file=sys.stderr)
    sys.exit(1)

pg_env = postgres.get('environment', {})
if isinstance(pg_env, list):
    pg_env_dict = {}
    for item in pg_env:
        k, _, v = str(item).partition('=')
        pg_env_dict[k] = v
    pg_env = pg_env_dict

if pg_env.get('PGDATA') != '/var/lib/postgresql/18/docker':
    print(f'ERROR: postgres PGDATA must be /var/lib/postgresql/18/docker, got: {pg_env.get(\"PGDATA\")}', file=sys.stderr)
    sys.exit(1)

pg_vols = [str(v) for v in postgres.get('volumes', [])]
if not any('postgres_data:/var/lib/postgresql' in v for v in pg_vols):
    print(f'ERROR: postgres volume postgres_data:/var/lib/postgresql missing, got: {pg_vols}', file=sys.stderr)
    sys.exit(1)

# Check postgres network is backend only
if postgres.get('networks') != ['backend']:
    print(f'ERROR: postgres networks must be [backend], got: {postgres.get(\"networks\")}', file=sys.stderr)
    sys.exit(1)

print('PASS: Test 3 - PostgreSQL 18 specifies PGDATA and persistent volume mount.')

# Test 4: Redis mounts ./deploy/redis/redis.conf and healthcheck script without inline passwords in command or healthcheck
redis = services.get('redis')
if not redis:
    print('ERROR: redis service is missing', file=sys.stderr)
    sys.exit(1)

redis_cmd = str(redis.get('command', ''))
if 'requirepass' in redis_cmd or '--requirepass' in redis_cmd:
    print(f'ERROR: inline password found in redis command: {redis_cmd}', file=sys.stderr)
    sys.exit(1)

redis_hc = str(redis.get('healthcheck', ''))
if 'requirepass' in redis_hc or '-a ' in redis_hc or '--auth' in redis_hc:
    print(f'ERROR: inline password found in redis healthcheck: {redis_hc}', file=sys.stderr)
    sys.exit(1)

redis_vols = [str(v) for v in redis.get('volumes', [])]
has_redis_conf = any('./deploy/redis/redis.conf:/usr/local/etc/redis/redis.conf:ro' in v for v in redis_vols)
has_redis_hc = any('./deploy/redis/redis-healthcheck.sh:/usr/local/bin/redis-healthcheck.sh:ro' in v for v in redis_vols)
if not (has_redis_conf and has_redis_hc):
    print(f'ERROR: redis missing config or healthcheck script mount. Found: {redis_vols}', file=sys.stderr)
    sys.exit(1)

if redis.get('networks') != ['backend']:
    print(f'ERROR: redis networks must be [backend], got: {redis.get(\"networks\")}', file=sys.stderr)
    sys.exit(1)

print('PASS: Test 4 - Redis mounts config and healthcheck script without inline passwords.')

# Test 5: Dex mounts ./deploy/dex/config.prod.yml
dex = services.get('dex')
if not dex:
    print('ERROR: dex service is missing', file=sys.stderr)
    sys.exit(1)

dex_vols = [str(v) for v in dex.get('volumes', [])]
has_dex_conf = any('./deploy/dex/config.prod.yml:/etc/dex/config.docker.yaml:ro' in v for v in dex_vols)
if not has_dex_conf:
    print(f'ERROR: dex missing config mount ./deploy/dex/config.prod.yml:/etc/dex/config.docker.yaml:ro. Found: {dex_vols}', file=sys.stderr)
    sys.exit(1)

if set(dex.get('networks', [])) != {'ingress', 'backend'}:
    print(f'ERROR: dex networks must be ingress and backend, got: {dex.get(\"networks\")}', file=sys.stderr)
    sys.exit(1)

print('PASS: Test 5 - Dex mounts production config.')

# Test 6: Control plane mounts Docker socket for orchestration but connects to backend data stores on internal network
cp = services.get('control-plane')
if not cp:
    print('ERROR: control-plane service is missing', file=sys.stderr)
    sys.exit(1)

cp_vols = [str(v) for v in cp.get('volumes', [])]
has_docker_sock = any('/var/run/docker.sock:/var/run/docker.sock' in v for v in cp_vols)
if not has_docker_sock:
    print(f'ERROR: control-plane must mount /var/run/docker.sock:/var/run/docker.sock, found: {cp_vols}', file=sys.stderr)
    sys.exit(1)

if set(cp.get('networks', [])) != {'ingress', 'backend'}:
    print(f'ERROR: control-plane networks must be ingress and backend, got: {cp.get(\"networks\")}', file=sys.stderr)
    sys.exit(1)

cp_env = cp.get('environment', {})
if isinstance(cp_env, list):
    cp_env_dict = {}
    for item in cp_env:
        k, _, v = str(item).partition('=')
        cp_env_dict[k] = v
    cp_env = cp_env_dict

if cp_env.get('APP_ENV') != 'production':
    print(f'ERROR: control-plane APP_ENV must be production, got: {cp_env.get(\"APP_ENV\")}', file=sys.stderr)
    sys.exit(1)

if cp_env.get('GIT_SSL_NO_VERIFY') != 'false':
    print(f'ERROR: control-plane GIT_SSL_NO_VERIFY must be \"false\", got: {cp_env.get(\"GIT_SSL_NO_VERIFY\")}', file=sys.stderr)
    sys.exit(1)

print('PASS: Test 6 - Control plane mounts Docker socket and connects across networks.')

# Test 7: Volumes defined
expected_vols = {'postgres_data', 'redis_data', 'traefik_acme'}
if not expected_vols.issubset(set(volumes.keys())):
    print(f'ERROR: Missing volumes: {expected_vols - set(volumes.keys())}', file=sys.stderr)
    sys.exit(1)

# Web UI checks
web_ui = services.get('web-ui')
if not web_ui:
    print('ERROR: web-ui service is missing', file=sys.stderr)
    sys.exit(1)

if web_ui.get('networks') != ['ingress']:
    print(f'ERROR: web-ui networks must be [ingress], got: {web_ui.get(\"networks\")}', file=sys.stderr)
    sys.exit(1)

print('PASS: All assertions verified successfully.')
"
