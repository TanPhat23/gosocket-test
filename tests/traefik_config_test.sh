#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TRAEFIK_CONFIG="${REPO_ROOT}/deploy/traefik/traefik.yml"
DYNAMIC_CONFIG="${REPO_ROOT}/deploy/traefik/dynamic.yml"

echo "=== Testing Traefik Configuration ==="

# 1. Existence check
if [[ ! -f "${TRAEFIK_CONFIG}" ]]; then
    echo "ERROR: ${TRAEFIK_CONFIG} not found" >&2
    exit 1
fi
if [[ ! -f "${DYNAMIC_CONFIG}" ]]; then
    echo "ERROR: ${DYNAMIC_CONFIG} not found" >&2
    exit 1
fi

# 2. PyYAML parsing check
echo "Checking YAML syntax with PyYAML..."
python3 -c "
import yaml, sys

for path in ['${TRAEFIK_CONFIG}', '${DYNAMIC_CONFIG}']:
    with open(path, 'r') as f:
        try:
            yaml.safe_load(f)
        except Exception as e:
            print(f'YAML parse error in {path}: {e}', file=sys.stderr)
            sys.exit(1)
print('Both YAML files parsed successfully.')
"

# 3. Test 1: deploy/traefik/traefik.yml completely omits providers.docker
echo "Checking that traefik.yml omits providers.docker..."
python3 -c "
import yaml, sys

with open('${TRAEFIK_CONFIG}', 'r') as f:
    data = yaml.safe_load(f)

providers = data.get('providers', {})
if 'docker' in providers:
    print('ERROR: providers.docker must be completely omitted from traefik.yml', file=sys.stderr)
    sys.exit(1)
print('PASS: providers.docker is omitted.')
"

# 4. Test 2: deploy/traefik/traefik.yml declares providers.file.filename: /etc/traefik/dynamic.yml
echo "Checking that traefik.yml declares providers.file.filename..."
python3 -c "
import yaml, sys

with open('${TRAEFIK_CONFIG}', 'r') as f:
    data = yaml.safe_load(f)

providers = data.get('providers', {})
file_prov = providers.get('file', {})
filename = file_prov.get('filename')
if filename != '/etc/traefik/dynamic.yml':
    print(f'ERROR: Expected providers.file.filename to be \"/etc/traefik/dynamic.yml\", got: \"{filename}\"', file=sys.stderr)
    sys.exit(1)

if not file_prov.get('watch', False):
    print('ERROR: Expected providers.file.watch to be True', file=sys.stderr)
    sys.exit(1)

print('PASS: providers.file configured correctly.')
"

# 5. Test 3: named entrypoints web (:80 with HTTPS redirect to websecure) and websecure (:443)
echo "Checking entrypoints configuration in traefik.yml..."
python3 -c "
import yaml, sys

with open('${TRAEFIK_CONFIG}', 'r') as f:
    data = yaml.safe_load(f)

entrypoints = data.get('entryPoints', {})
web = entrypoints.get('web', {})
websecure = entrypoints.get('websecure', {})

if web.get('address') != ':80':
    print(f'ERROR: web entrypoint address must be \":80\", got: \"{web.get(\"address\")}\"', file=sys.stderr)
    sys.exit(1)

redir = web.get('http', {}).get('redirections', {}).get('entryPoint', {})
if redir.get('to') != 'websecure' or redir.get('scheme') != 'https':
    print(f'ERROR: web entrypoint must redirect to websecure scheme https, got: {redir}', file=sys.stderr)
    sys.exit(1)

if websecure.get('address') != ':443':
    print(f'ERROR: websecure entrypoint address must be \":443\", got: \"{websecure.get(\"address\")}\"', file=sys.stderr)
    sys.exit(1)

tls_resolver = websecure.get('http', {}).get('tls', {}).get('certResolver')
if tls_resolver != 'letsencrypt':
    print(f'ERROR: websecure certResolver must be letsencrypt, got: \"{tls_resolver}\"', file=sys.stderr)
    sys.exit(1)

print('PASS: entryPoints web and websecure configured correctly.')
"

# 6. Test 4: dynamic.yml declares explicit router priorities (priority: 100 for control-plane and dex; priority: 10 for web-ui)
echo "Checking dynamic.yml routers and priorities..."
python3 -c "
import yaml, sys

with open('${DYNAMIC_CONFIG}', 'r') as f:
    data = yaml.safe_load(f)

routers = data.get('http', {}).get('routers', {})

for r_name in ['web-ui', 'control-plane', 'dex']:
    if r_name not in routers:
        print(f'ERROR: Router \"{r_name}\" missing from dynamic.yml', file=sys.stderr)
        sys.exit(1)

web_ui = routers['web-ui']
cp = routers['control-plane']
dex = routers['dex']

if web_ui.get('priority') != 10:
    print(f'ERROR: web-ui priority must be 10, got: {web_ui.get(\"priority\")}', file=sys.stderr)
    sys.exit(1)

if cp.get('priority') != 100:
    print(f'ERROR: control-plane priority must be 100, got: {cp.get(\"priority\")}', file=sys.stderr)
    sys.exit(1)

if dex.get('priority') != 100:
    print(f'ERROR: dex priority must be 100, got: {dex.get(\"priority\")}', file=sys.stderr)
    sys.exit(1)

# Check services
services = data.get('http', {}).get('services', {})
for s_name in ['web-ui-service', 'control-plane-service', 'dex-service']:
    if s_name not in services:
        print(f'ERROR: Service \"{s_name}\" missing from dynamic.yml', file=sys.stderr)
        sys.exit(1)

print('PASS: dynamic.yml routers, priorities, and services configured correctly.')
"

echo "=== All Traefik configuration tests PASSED ==="
