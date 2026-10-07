#!/usr/bin/env bash
# Run on the host with the production kubeconfig available.
set -euo pipefail
context=${1:-}
if [[ -z "$context" || $# != 1 ]]; then
  echo 'Usage: bash scripts/check-prod.sh PRODUCTION_CONTEXT' >&2
  exit 1
fi
for tool in kubectl flux; do
  command -v "$tool" >/dev/null || { echo "Required tool missing: $tool" >&2; exit 1; }
done
bash "$(dirname -- "${BASH_SOURCE[0]}")/reconcile-gitops.sh" "$context"
flux --context "$context" -n flux-system reconcile kustomization rundt-herbsfest-web
kubectl --context "$context" -n rundt wait --for=condition=Ready clusters.postgresql.cnpg.io/herbsfest-postgres --timeout=600s
kubectl --context "$context" -n rundt rollout status deployment/herbsfest --timeout=600s
kubectl --context "$context" -n rundt wait --for=condition=Ready certificate/herbsfest-tls --timeout=300s
kubectl --context "$context" -n rundt get deployment herbsfest \
  -o jsonpath='{.spec.template.spec.containers[?(@.name=="web")].image}{"\n"}'
