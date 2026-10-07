#!/usr/bin/env bash
# Host-side operations. All cluster access specifies the supplied context.
set -euo pipefail
set +x
umask 077
cd "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

action=${1:-}
context=${2:-}
case "$action" in
  register|check|password|reseal) ;;
  *) echo 'Usage: bash scripts/zitadel.sh {register|check|password|reseal} PRODUCTION_CONTEXT' >&2; exit 1 ;;
esac
if [[ -z "$context" ]]; then
  echo 'A production context is required.' >&2
  exit 1
fi
require() {
  command -v "$1" >/dev/null || { echo "Required tool missing: $1" >&2; exit 1; }
}
require kubectl
kube=(kubectl --context "$context")
"${kube[@]}" get namespace rundt >/dev/null

case "$action" in
  register)
    # Compatibility alias: the central inventory now registers every app.
    require kubeseal
    "${kube[@]}" -n flux-system get gitrepository herbsfest >/dev/null
    kubeseal --context "$context" --controller-name=sealed-secrets-controller \
      --controller-namespace=sealed-secrets --validate \
      < config/zitadel/sealedsecret-bootstrap.yaml
    bash scripts/reconcile-gitops.sh "$context"
    ;;
  check)
    require flux
    require curl
    bash scripts/reconcile-gitops.sh "$context"
    flux --context "$context" reconcile kustomization rundt-zitadel \
      --namespace flux-system --with-source
    "${kube[@]}" -n rundt wait clusters.postgresql.cnpg.io/zitadel-postgres \
      --for=condition=Ready --timeout=15m
    "${kube[@]}" -n rundt wait helmrelease/zitadel \
      --for=condition=Ready --timeout=20m
    "${kube[@]}" -n rundt rollout status deployment/zitadel --timeout=5m
    "${kube[@]}" -n rundt rollout status deployment/zitadel-login --timeout=5m
    "${kube[@]}" -n rundt wait certificate/zitadel-tls \
      --for=condition=Ready --timeout=5m
    curl --fail --silent --show-error \
      https://auth.r-und-t.app/.well-known/openid-configuration
    printf '\n'
    ;;
  password)
    require python3
    # This action intentionally shows only the initial password on the host.
    # Use it privately; the running account password changes on first login.
    "${kube[@]}" -n rundt get secret zitadel-bootstrap -o json |
      python3 -c 'import base64,json,sys; print(base64.b64decode(json.load(sys.stdin)["data"]["admin-password"]).decode())'
    ;;
  reseal)
    require kubeseal
    mkdir -p dist
    trap 'rm -f dist/zitadel-bootstrap.sealed.yaml.tmp dist/zitadel-cert.pem.tmp' EXIT
    kubeseal --context "$context" --fetch-cert \
      --controller-name=sealed-secrets-controller \
      --controller-namespace=sealed-secrets > dist/zitadel-cert.pem.tmp
    # Re-encrypt the existing Secret. Never generate a replacement masterkey.
    # Only public certificate and ciphertext reach disk.
    "${kube[@]}" -n rundt get secret zitadel-bootstrap -o json |
      kubeseal --cert dist/zitadel-cert.pem.tmp --scope strict --format yaml \
      > dist/zitadel-bootstrap.sealed.yaml.tmp
    kubeseal --context "$context" --controller-name=sealed-secrets-controller \
      --controller-namespace=sealed-secrets --validate \
      < dist/zitadel-bootstrap.sealed.yaml.tmp
    mv dist/zitadel-cert.pem.tmp dist/sealed-secrets-cert.pem
    mv dist/zitadel-bootstrap.sealed.yaml.tmp config/zitadel/sealedsecret-bootstrap.yaml
    echo 'Existing bootstrap values resealed; commit config/zitadel/sealedsecret-bootstrap.yaml.'
    ;;
esac
