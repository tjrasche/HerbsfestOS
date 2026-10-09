#!/usr/bin/env bash
# Host-side production checks; the API is reachable only through port forwarding.
set -euo pipefail
set +x
umask 077
cd "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

action=${1:-}
context=${2:-}
if [[ $# != 2 || -z "$context" || ! "$action" =~ ^(check|forward)$ ]]; then
  echo 'Usage: bash scripts/openfga.sh {check|forward} PRODUCTION_CONTEXT' >&2
  exit 1
fi
require() {
  command -v "$1" >/dev/null || { echo "Required tool missing: $1" >&2; exit 1; }
}
require kubectl
kube=(kubectl --context "$context" -n rundt)

if [[ "$action" == forward ]]; then
  exec "${kube[@]}" port-forward --address 127.0.0.1 service/openfga 8180:8080
fi

require flux
require python3
bash scripts/reconcile-gitops.sh "$context"
flux --context "$context" -n flux-system reconcile kustomization rundt-openfga --with-source
"${kube[@]}" wait --for=condition=Ready clusters.postgresql.cnpg.io/openfga-postgres --timeout=600s
"${kube[@]}" rollout status deployment/openfga --timeout=600s

forward_log=$(mktemp)
forward_pid=''
cleanup() {
  if [[ -n "$forward_pid" ]]; then
    kill "$forward_pid" 2>/dev/null || true
    wait "$forward_pid" 2>/dev/null || true
  fi
  rm -f "$forward_log"
}
trap cleanup EXIT
"${kube[@]}" port-forward --address 127.0.0.1 service/openfga 8180:8080 > "$forward_log" 2>&1 &
forward_pid=$!
forward_ready=false
for ((attempt = 0; attempt < 100; attempt++)); do
  if ! kill -0 "$forward_pid" 2>/dev/null; then
    cat "$forward_log" >&2
    exit 1
  fi
  if [[ $(< "$forward_log") == *'Forwarding from 127.0.0.1:8180'* ]]; then
    forward_ready=true
    break
  fi
  sleep 0.2
done
if [[ "$forward_ready" != true ]]; then
  echo 'Timed out waiting for the OpenFGA port forward.' >&2
  exit 1
fi

# Retrieve the API key into process memory. Never print it or write it to disk.
python3 - "$context" <<'PY'
import base64
import json
import subprocess
import sys
import urllib.error
import urllib.request

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None

def main():
    result = subprocess.run(
        ["kubectl", "--context", sys.argv[1], "-n", "rundt", "get", "secret",
         "openfga-api-auth", "-o", "json"],
        check=True, capture_output=True,
    )
    token = base64.b64decode(json.loads(result.stdout)["data"]["API_TOKEN"], validate=True).decode()
    if not token:
        raise ValueError("API token is missing")
    http = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    with http.open("http://127.0.0.1:8180/healthz", timeout=5) as response:
        if json.load(response).get("status") != "SERVING":
            raise ValueError("OpenFGA is not serving")
    url = "http://127.0.0.1:8180/stores?page_size=1"
    try:
        http.open(url, timeout=5).close()
    except urllib.error.HTTPError as error:
        if error.code != 401:
            raise ValueError("Unexpected response to unauthenticated API request") from None
    else:
        raise ValueError("OpenFGA accepted an unauthenticated API request")
    request = urllib.request.Request(url, headers={"Authorization": "Bearer " + token})
    with http.open(request, timeout=5) as response:
        if not isinstance(json.load(response).get("stores"), list):
            raise ValueError("Unexpected authenticated API response")
    print("OpenFGA is serving; authenticated API access works and anonymous API access is rejected.")

try:
    main()
except (OSError, ValueError, KeyError, subprocess.CalledProcessError, urllib.error.URLError):
    print("OpenFGA connectivity or authentication check failed. Check the Deployment, Secret and port forward.", file=sys.stderr)
    sys.exit(1)
PY
