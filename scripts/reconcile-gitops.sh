#!/usr/bin/env bash
# Run on the host with the production kubeconfig available.
set -euo pipefail
context=${1:-}
if [[ -z "$context" || $# != 1 ]]; then
  echo 'Usage: bash scripts/reconcile-gitops.sh PRODUCTION_CONTEXT' >&2
  exit 1
fi
for tool in kubectl flux; do
  command -v "$tool" >/dev/null || { echo "Required tool missing: $tool" >&2; exit 1; }
done
path=$(kubectl --context "$context" -n flux-system get kustomization rundt-herbsfest \
  -o jsonpath='{.spec.path}')
if [[ "$path" != './config/flux' ]]; then
  echo 'The infra-owned rundt-herbsfest entry must point to ./config/flux. See config/flux/README.md.' >&2
  exit 1
fi
flux --context "$context" -n flux-system reconcile kustomization rundt-herbsfest --with-source
