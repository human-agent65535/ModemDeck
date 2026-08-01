#!/bin/sh

# GitHub expressions are intentionally matched as literal single-quoted text.
# shellcheck disable=SC2016

set -eu

tests_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH='' cd -- "${tests_dir}/../.." && pwd)
workflow="${repo_dir}/.github/workflows/publish-images.yml"

fail() {
    printf 'release-workflow-test: %s\n' "$*" >&2
    exit 1
}

[ -f "$workflow" ] || fail "release image workflow is missing"

grep -Fq '      - "v[0-9]+.[0-9]+.[0-9]+"' "$workflow" ||
    fail "workflow is not restricted to stable release tags"
grep -Fq '  workflow_dispatch:' "$workflow" ||
    fail "workflow cannot backfill an existing stable release"
grep -Fq '      release_tag:' "$workflow" ||
    fail "release backfill does not require an explicit tag"
grep -Fq '      include_hardware:' "$workflow" ||
    fail "release backfill cannot explicitly include Hardware"
grep -Fq 'RELEASE_TAG} does not match VERSION' "$workflow" ||
    fail "workflow does not require the tag to match VERSION"
grep -Fq 'git cat-file -t "${RELEASE_REF}"' "$workflow" ||
    fail "workflow does not require an annotated release tag"

for permission in \
    'attestations: write' \
    'contents: read' \
    'id-token: write' \
    'packages: write'
do
    grep -Fq "$permission" "$workflow" ||
        fail "workflow permission is missing: ${permission}"
done

for matrix_contract in \
    '"component":"api","dockerfile":"./Dockerfile","image":"modemdeck","target":"runtime"' \
    '"component":"web","dockerfile":"./Dockerfile","image":"modemdeck-web","target":"web-runtime"' \
    '"component":"updater","dockerfile":"./Dockerfile","image":"modemdeck-updater","target":"updater-runtime"' \
    '"component":"hardware","dockerfile":"./hardware/Dockerfile","image":"modemdeck-hardware","target":"runtime"'
do
    grep -Fq "$matrix_contract" "$workflow" ||
        fail "release image contract is incomplete: ${matrix_contract}"
done

grep -Fq 'git diff --quiet "${previous_tag}^{commit}" "${vcs_ref}" -- agent hardware' \
    "$workflow" || fail "automatic releases do not isolate Hardware source changes"
grep -Fq 'publish_hardware="${INCLUDE_HARDWARE:-false}"' "$workflow" ||
    fail "manual release backfills do not default to application images only"
grep -Fq "jq -r '.hardware_version // \"\"' deploy/release-manifest.json" \
    "$workflow" || fail "release workflow does not validate the retained Hardware version"
grep -Fq 'a Hardware-changing release must set hardware_version' "$workflow" ||
    fail "Hardware image publication is not tied to the release manifest"
grep -Fq 'release manifest must pin the tested Cloudflared tag and sha256 digest' \
    "$workflow" || fail "release workflow does not validate the Cloudflared pin"
grep -Fq 'matrix: ${{ fromJSON(needs.release.outputs.image-matrix) }}' "$workflow" ||
    fail "release jobs do not use the validated component matrix"

grep -Fq 'platforms: linux/amd64,linux/arm64' "$workflow" ||
    fail "release images are not multi-architecture"
grep -Fq 'subject-digest: ${{ steps.build.outputs.digest }}' "$workflow" ||
    fail "release image digest is not attested"
grep -Fq 'org.opencontainers.image.source=https://github.com/${{ github.repository }}' \
    "$workflow" || fail "release images are not linked to their source repository"
grep -Fq '${{ env.IMAGE }}:sha-${{ needs.release.outputs.vcs-ref }}' "$workflow" ||
    fail "release images do not have an immutable source revision tag"
grep -Fq '          ref: ${{ needs.release.outputs.vcs-ref }}' "$workflow" ||
    fail "image builds do not check out the validated release commit"

if grep -Eq '^ +[^#]*IMAGE.*:latest([[:space:]]|$)' "$workflow"; then
    fail "release workflow must not publish a mutable latest tag"
fi
if grep -Fq 'pull_request_target:' "$workflow"; then
    fail "release workflow must not run privileged publishing from pull requests"
fi

awk '
    /^[[:space:]]*uses:/ {
        ref = $0
        sub(/^.*@/, "", ref)
        sub(/[[:space:]]+#.*$/, "", ref)
        if (ref !~ /^[0-9a-f]{40}$/) {
            print "unpinned action: " $0 > "/dev/stderr"
            invalid = 1
        }
    }
    END { exit invalid }
' "$workflow" || fail "every release action must be pinned to a commit SHA"

printf '%s\n' "release-workflow-test: ok"
