#!/usr/bin/env bash
# Run on the host, where the production kubeconfig and registry login are available.
set -euo pipefail
# Never allow an inherited xtrace setting to print the auth password.
set +x
umask 077

cd "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
action=${1:-}
context=${2:-}
if [[ ! "$action" =~ ^(cert|seal|deploy)$ || -z "$context" ]]; then
  echo "Usage: bash scripts/prod.sh {cert|seal|deploy} PRODUCTION_CONTEXT" >&2
  exit 1
fi

require() {
  command -v "$1" >/dev/null || { echo "Required tool missing: $1" >&2; exit 1; }
}
require kubectl
require kubeseal
kube=(kubectl --context "$context")
"${kube[@]}" get namespace rundt >/dev/null
mkdir -p dist

fetch_cert() {
  kubeseal --context "$context" --fetch-cert \
    --controller-name=sealed-secrets-controller \
    --controller-namespace=sealed-secrets > dist/sealed-secrets-cert.pem.tmp
  mv dist/sealed-secrets-cert.pem.tmp dist/sealed-secrets-cert.pem
  echo 'Public certificate written to dist/sealed-secrets-cert.pem'
}

seal_auth() {
  require htpasswd
  fetch_cert
  local auth_password
  read -r -s -p 'Basic-auth password for mvr: ' auth_password
  printf '\n' >&2
  if [[ -z "$auth_password" ]]; then
    echo 'Password must not be empty.' >&2
    return 1
  fi
  # Only the encrypted output reaches disk; htpasswd and Secret JSON stay in pipes.
  printf '%s\n' "$auth_password" | htpasswd -niB -C 10 mvr |
    "${kube[@]}" -n rundt create secret generic herbsfest-basic-auth \
      --from-file=users=/dev/stdin --dry-run=client -o json |
    kubeseal --cert dist/sealed-secrets-cert.pem --scope strict --format yaml \
      > dist/herbsfest-basic-auth.sealed.yaml.tmp
  unset auth_password
  mv dist/herbsfest-basic-auth.sealed.yaml.tmp dist/herbsfest-basic-auth.sealed.yaml
  echo 'Sealed credentials written to dist/herbsfest-basic-auth.sealed.yaml'
}

case "$action" in
  cert) fetch_cert ;;
  seal) seal_auth ;;
  deploy)
    for tool in go make ko kustomize; do require "$tool"; done
    "${kube[@]}" -n rundt get secret oci-registry-cred >/dev/null
    "${kube[@]}" get storageclass longhorn >/dev/null
    "${kube[@]}" get clusterissuer bunnycdn-issuer >/dev/null
    # Validate existing ciphertext against the selected cluster before applying it.
    if [[ ! -s dist/herbsfest-basic-auth.sealed.yaml ]]; then seal_auth; fi
    kubeseal --context "$context" --controller-name=sealed-secrets-controller \
      --controller-namespace=sealed-secrets --validate < dist/herbsfest-basic-auth.sealed.yaml
    prod_platform=$("${kube[@]}" get nodes -o jsonpath='{.items[0].status.nodeInfo.operatingSystem}/{.items[0].status.nodeInfo.architecture}')
    test -n "$prod_platform"
    make prod-resolve PROD_PLATFORM="$prod_platform"
    "${kube[@]}" apply -f dist/prod.yaml
    "${kube[@]}" -n rundt wait --for=condition=Ready cluster/herbsfest-postgres --timeout=600s
    "${kube[@]}" -n rundt rollout status deployment/herbsfest --timeout=600s
    "${kube[@]}" -n rundt wait --for=condition=Ready certificate/herbsfest-tls --timeout=300s
    echo 'Deployed: https://herbstfest.r-und-t.app (basic-auth user: mvr)'
    ;;
esac
