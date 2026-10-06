#!/usr/bin/env bash
# Called by CI after ko has successfully published the image.
set -euo pipefail

source_sha=${1:-}
image_ref=${2:-}
if [[ ! "$source_sha" =~ ^[0-9a-f]{40}$ || ! "$image_ref" =~ ^artifacts\.r-und-t\.app/herbsfest@sha256:[0-9a-f]{64}$ ]]; then
  echo 'Usage: bash scripts/publish-manifest.sh SOURCE_SHA artifacts.r-und-t.app/herbsfest@sha256:DIGEST' >&2
  exit 1
fi
cd "$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
test "$(git rev-parse HEAD)" = "$source_sha"

# Never let a queued build replace the deployment requested by a newer commit.
git fetch origin main
if [[ "$(git rev-parse origin/main)" != "$source_sha" ]]; then
  echo 'Image published; main has advanced, so this build will not change production.'
  exit 0
fi

(
  cd config/overlays/prod
  kustomize edit set image "ko://github.com/tjrasche/HerbsfestOS/cmd/web=$image_ref"
)
kustomize build config/overlays/prod > /dev/null
git diff --check
git add config/overlays/prod/kustomization.yaml
if git diff --cached --quiet; then
  echo 'Production already uses this image digest.'
  exit 0
fi
git config user.name 'github-actions[bot]'
git config user.email '41898282+github-actions[bot]@users.noreply.github.com'
git commit -m "chore(deploy): publish app ${source_sha:0:12} [skip ci]"

for attempt in 1 2 3 4 5; do
  if git push origin HEAD:main; then
    if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
      # shellcheck disable=SC2016 # Backticks are Markdown formatting.
      printf 'Production image: `%s`\n\nFlux will reconcile `config/overlays/prod`.\n' "$image_ref" >> "$GITHUB_STEP_SUMMARY"
    fi
    exit 0
  fi
  git fetch origin main
  if [[ "$(git rev-parse origin/main)" != "$source_sha" ]]; then
    echo 'Main advanced before the digest could be pushed; keeping the newer deployment.'
    exit 0
  fi
  if [[ "$attempt" == 5 ]]; then exit 1; fi
done
