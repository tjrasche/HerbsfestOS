#!/usr/bin/env bash
# The caller's gh authentication needs Actions: write on the GitOps repository.
set -euo pipefail
set +x

sha=${1:-}
if [[ ! "$sha" =~ ^[0-9a-f]{40}$ ]]; then
  echo 'Usage: bash scripts/dispatch-build.sh APP_COMMIT_SHA' >&2
  exit 1
fi

repo=rasche-thalhofer/k8s-infra
response=$(gh api --method POST \
  -H 'X-GitHub-Api-Version: 2026-03-10' \
  "repos/$repo/actions/workflows/herbsfest-ko-build.yml/dispatches" \
  -f ref=main -f "inputs[source_ref]=$sha" -F 'inputs[automatic]=true')
run_id=$(jq -er '.workflow_run_id' <<< "$response")
url=$(jq -er '.html_url' <<< "$response")
printf 'Registry build and GitOps update: %s\n' "$url"
if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  printf '[Registry build and GitOps update](%s) for %s.\n' "$url" "$sha" >> "$GITHUB_STEP_SUMMARY"
fi
# Poll the Actions API so the token needs no additional Checks permission.
deadline=$((SECONDS + 2400))
previous_status=
while (( SECONDS < deadline )); do
  run=$(gh api "repos/$repo/actions/runs/$run_id")
  status=$(jq -er '.status' <<< "$run")
  if [[ "$status" != "$previous_status" ]]; then
    printf 'Downstream workflow: %s\n' "$status"
    previous_status=$status
  fi
  if [[ "$status" == completed ]]; then
    conclusion=$(jq -er '.conclusion' <<< "$run")
    if [[ "$conclusion" == success ]]; then
      echo 'Image publishing and GitOps update succeeded.'
      exit 0
    fi
    printf 'Downstream workflow failed (%s): %s\n' "$conclusion" "$url" >&2
    exit 1
  fi
  sleep 15
done
printf 'Timed out waiting for the downstream workflow: %s\n' "$url" >&2
exit 1
