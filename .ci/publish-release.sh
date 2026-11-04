#!/usr/bin/env bash
# Run after all build artifacts have been downloaded and SHA-256 verified.
set -euo pipefail
: "${GH_REPO:?Missing GitHub repository}"
: "${RELEASE_TAG:?Missing release tag}"
: "${RELEASE_COMMIT:?Missing build commit}"

retry() {
  local attempt
  for attempt in 1 2 3 4; do
    if "$@"; then return 0; fi
    if [[ $attempt == 4 ]]; then return 1; fi
    printf 'GitHub request failed; retrying (%s/4)…\n' "$attempt" >&2
    sleep "$((attempt * 2))"
  done
}

# A failed API response must not be mistaken for a missing tag or release.
refs=$(retry gh api "repos/$GH_REPO/git/matching-refs/tags/$RELEASE_TAG")
tag_sha=$(jq -r --arg ref "refs/tags/$RELEASE_TAG" '.[] | select(.ref == $ref) | .object.sha' <<< "$refs")
if [[ -z "$tag_sha" ]]; then
  # Do not retry a non-idempotent create blindly: the server may have accepted it.
  if ! gh api --method POST "repos/$GH_REPO/git/refs" \
    -f "ref=refs/tags/$RELEASE_TAG" -f "sha=$RELEASE_COMMIT" >/dev/null; then
    echo 'Tag creation returned an error; checking its actual state.' >&2
  fi
fi
resolved_commit=$(retry gh api "repos/$GH_REPO/commits/$RELEASE_TAG" --jq .sha)
if [[ "$resolved_commit" != "$RELEASE_COMMIT" ]]; then
  echo "Tag $RELEASE_TAG points to another commit; run on that tag or use a new version." >&2
  exit 1
fi

# Listing includes drafts for the authenticated repository token. Paginate so
# an API outage is never interpreted as permission to create another release.
find_release() {
  local releases
  releases=$(retry gh api --paginate --slurp "repos/$GH_REPO/releases?per_page=100") || return 1
  jq -c --arg tag "$RELEASE_TAG" '[.[][] | select(.tag_name == $tag)] | if length > 1 then error("Duplicate release tag") else .[0] // empty end' <<< "$releases"
}
release=$(find_release)
prerelease=()
if [[ "$RELEASE_TAG" == *-* ]]; then prerelease=(--prerelease); fi
if [[ -z "$release" ]]; then
  if ! gh release create "$RELEASE_TAG" --verify-tag --draft "${prerelease[@]}" \
    --target "$RELEASE_COMMIT" --title "RMic $RELEASE_TAG" \
    --notes 'Windows/Linux provide separate GUI and TUI packages. Windows includes only the unchanged base VB-CABLE ZIP for manual installation. Android APKs marked release-unsigned require signing; debug builds are for testing. SHA-256 files accompany all packages. See README for setup and platform validation limits.'; then
    echo 'Release creation returned an error; checking for a saved draft.' >&2
  fi
  release=$(find_release)
fi
if [[ -z "$release" ]]; then
  echo 'No release draft found. Re-run failed jobs to retry publishing.' >&2
  exit 1
fi
if [[ $(jq -r .draft <<< "$release") != true ]]; then
  echo 'Release already published; no assets were changed. Use a new version tag.' >&2
  exit 1
fi

# Re-uploading with --clobber is safe while the release remains a draft.
retry gh release upload "$RELEASE_TAG" dist/* --clobber
release_id=$(jq -r .id <<< "$release")
# Publishing may succeed even if the response fails. Check state before retrying.
for attempt in 1 2 3 4; do
  if gh release edit "$RELEASE_TAG" --tag "$RELEASE_TAG" --target "$RELEASE_COMMIT" \
    --draft=false "${prerelease[@]}"; then break; fi
  state=$(retry gh api "repos/$GH_REPO/releases/$release_id")
  if [[ $(jq -r .draft <<< "$state") == false && $(jq -r .tag_name <<< "$state") == "$RELEASE_TAG" ]]; then
    echo 'Release was published despite the failed API response.'
    break
  fi
  if [[ $attempt == 4 ]]; then
    echo 'Publishing failed; the draft and uploaded assets are retained for retry.' >&2
    exit 1
  fi
  sleep "$((attempt * 2))"
done
retry gh api "repos/$GH_REPO/releases/$release_id" --jq .html_url
