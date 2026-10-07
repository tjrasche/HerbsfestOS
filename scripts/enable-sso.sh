#!/usr/bin/env bash
# Verify the deployed Go auth guards before preparing removal of ingress Basic Auth.
set -euo pipefail
set +x

if [[ $# -ne 1 || -z $1 ]]; then
  echo "Usage: bash scripts/enable-sso.sh PRODUCTION_CONTEXT" >&2
  exit 1
fi
for tool in kubectl curl python3; do
  command -v "$tool" >/dev/null || { echo "Required tool missing: $tool" >&2; exit 1; }
done
repo_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
production_context=$1
forward_port=${SSO_FORWARD_PORT:-18080}
[[ $forward_port =~ ^[0-9]+$ ]] || { echo 'SSO_FORWARD_PORT must be numeric.' >&2; exit 1; }
check_dir=$(mktemp -d)
forward_pid=
cleanup() {
  if [[ -n $forward_pid ]]; then
    kill "$forward_pid" 2>/dev/null || true
    wait "$forward_pid" 2>/dev/null || true
  fi
  rm -rf "$check_dir"
}
trap cleanup EXIT

kubectl --context "$production_context" -n rundt rollout status deployment/herbsfest --timeout=180s
kubectl --context "$production_context" -n rundt port-forward --address 127.0.0.1 deployment/herbsfest "$forward_port:8080" >"$check_dir/forward.log" 2>&1 &
forward_pid=$!
forward_ready=false
for ((attempt=0; attempt<50; attempt++)); do
  if ! kill -0 "$forward_pid" 2>/dev/null; then
    cat "$check_dir/forward.log" >&2
    exit 1
  fi
  # Check this forwarder's output so an unrelated process on the port cannot pass.
  if forward_started=$(python3 - "$check_dir/forward.log" <<'PY'
import pathlib, sys
print("yes" if "Forwarding from 127.0.0.1:" in pathlib.Path(sys.argv[1]).read_text() else "no")
PY
  ); [[ $forward_started == yes ]]; then
    forward_ready=true
    break
  fi
  sleep 0.2
done
[[ $forward_ready == true ]] || { cat "$check_dir/forward.log" >&2; echo 'Port forward did not start.' >&2; exit 1; }

for path in / /design-system /auth/login; do
  case "$path" in
    /) name=feedback ;;
    /design-system) name=design ;;
    /auth/login) name=login ;;
  esac
  curl --silent --show-error --max-time 10 --dump-header "$check_dir/$name.headers" --output /dev/null "http://127.0.0.1:$forward_port$path"
done
curl --silent --show-error --max-time 10 --request POST --dump-header "$check_dir/create.headers" --output /dev/null "http://127.0.0.1:$forward_port/feedback-notes"

python3 - "$repo_dir" "$check_dir" <<'PY'
import pathlib, sys, urllib.parse
repo, checks = map(pathlib.Path, sys.argv[1:])
def headers(name):
    lines = (checks / (name + ".headers")).read_text().splitlines()
    status = int(lines[0].split()[1])
    values = dict(line.split(":", 1) for line in lines[1:] if ":" in line)
    return status, {key.lower(): value.strip() for key, value in values.items()}
for name in ("feedback", "design", "create"):
    status, response = headers(name)
    if status != 303 or response.get("location") != "/auth/login":
        sys.exit("Deployed app does not enforce the expected OIDC guard. Keep Basic Auth and wait for the new image.")
status, response = headers("login")
login = urllib.parse.urlparse(response.get("location", ""))
query = urllib.parse.parse_qs(login.query)
expected = {
    "client_id": ["394051797699788961"],
    "redirect_uri": ["https://herbstfest.r-und-t.app/auth/callback"],
    "response_type": ["code"],
    "code_challenge_method": ["S256"],
}
if (status != 302 or login.scheme != "https" or login.netloc != "auth.r-und-t.app"
        or login.path != "/oauth/v2/authorize" or any(query.get(k) != v for k, v in expected.items())
        or not query.get("nonce") or not query.get("state") or not query.get("code_challenge")
        or "urn:zitadel:iam:org:project:id:394051632024780961:aud" not in query.get("scope", [""])[0].split()):
    sys.exit("Deployed OIDC configuration does not match the production client. Keep Basic Auth.")
path = repo / "config/overlays/prod/ingress.yaml"
text = path.read_text()
basic_auth = ",rundt-herbsfest-basic-auth@kubernetescrd"
private_cache = "rundt-herbsfest-private-cache@kubernetescrd"
if private_cache not in text:
    sys.exit("Unexpected ingress configuration; no changes made.")
if basic_auth in text:
    path.write_text(text.replace(basic_auth, ""))
    print("Verified deployed OIDC guards. Prepared ingress.yaml to remove Basic Auth.")
else:
    print("Verified deployed OIDC guards. Basic Auth is already absent from ingress.yaml.")
print("Review the diff, then commit and push the ingress change for Flux to apply.")
print("After reconciliation, sign in at https://herbstfest.r-und-t.app/ using a user with the project role club-member.")
PY
