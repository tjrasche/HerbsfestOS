#!/usr/bin/env bash
# Run on your host with gh authenticated and the GitHub App key available locally.
set -euo pipefail
set +x

if (( $# != 0 && $# != 2 )); then
  echo 'Usage: bash scripts/configure-ci.sh [APP_ID PRIVATE_KEY_FILE]' >&2
  exit 1
fi

command -v gh >/dev/null || { echo 'Install the GitHub CLI (gh) first.' >&2; exit 1; }
gh api user --jq '.login' >/dev/null

gitops_app_id=${1:-}
gitops_key_file=${2:-}
if [[ -z "$gitops_app_id" ]]; then
  read -r -p 'GitHub App ID: ' gitops_app_id
fi
if [[ ! "$gitops_app_id" =~ ^[0-9]+$ ]]; then
  echo 'The GitHub App ID must be numeric.' >&2
  exit 1
fi
if [[ -z "$gitops_key_file" ]]; then
  read -r -p 'Path to the GitHub App private key (.pem): ' gitops_key_file
fi
if [[ "$gitops_key_file" == \~/* ]]; then
  gitops_key_file="$HOME/${gitops_key_file:2}"
fi
if [[ ! -f "$gitops_key_file" || ! -r "$gitops_key_file" ]]; then
  echo 'The private key file must exist and be readable.' >&2
  exit 1
fi
IFS= read -r gitops_key_header < "$gitops_key_file"
case "${gitops_key_header%$'\r'}" in
  '-----BEGIN RSA PRIVATE KEY-----'|'-----BEGIN PRIVATE KEY-----') ;;
  *)
    echo 'Select the GitHub App private key PEM, rather than a public certificate.' >&2
    exit 1
    ;;
esac

echo 'The App must be installed on rasche-thalhofer/k8s-infra with Actions: read and write.'
printf '%s' "$gitops_app_id" | gh secret set GITOPS_APP_ID --repo tjrasche/HerbsfestOS
gh secret set GITOPS_APP_PRIVATE_KEY --repo tjrasche/HerbsfestOS < "$gitops_key_file"
echo 'Configured app CI secrets. Triggering CI on main.'
gh workflow run ci.yml --repo tjrasche/HerbsfestOS --ref main
echo 'Follow CI at https://github.com/tjrasche/HerbsfestOS/actions/workflows/ci.yml'
