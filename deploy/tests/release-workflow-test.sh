#!/bin/sh

# GitHub expressions are intentionally matched as literal single-quoted text.
# shellcheck disable=SC2016

set -eu

tests_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(CDPATH='' cd -- "${tests_dir}/../.." && pwd)
workflow="${repo_dir}/.github/workflows/publish-images.yml"
planner="${repo_dir}/deploy/release-image-plan.sh"

fail() {
    printf 'release-workflow-test: %s\n' "$*" >&2
    exit 1
}

[ -f "$workflow" ] || fail "release image workflow is missing"
[ -x "$planner" ] || fail "release image planner is missing or not executable"

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
    'image=modemdeck' \
    'image=modemdeck-web' \
    'image=modemdeck-updater' \
    'image=modemdeck-hardware' \
    'target=web-runtime' \
    'target=updater-runtime'
do
    grep -Fq "$matrix_contract" "$planner" ||
        fail "release image contract is incomplete: ${matrix_contract}"
done

grep -Fq 'deploy/release-image-plan.sh' "$workflow" ||
    fail "release workflow does not use the component planner"
grep -Fq 'for component in api web updater hardware' "$planner" ||
    fail "planner does not evaluate every component independently"
grep -Fq '"${1}_version"' "$planner" ||
    fail "planner does not read per-component manifest versions"
grep -Fq 'internal/ota/**' "$planner" ||
    fail "updater source changes are not isolated"
grep -Fq 'web/src/**' "$planner" ||
    fail "Web source changes are not isolated"
grep -Fq 'agent/**' "$planner" ||
    fail "Hardware source changes are not isolated"
grep -Fq 'release manifest must pin the tested Cloudflared tag and sha256 digest' \
    "$workflow" || fail "release workflow does not validate the Cloudflared pin"
grep -Fq 'matrix: ${{ fromJSON(needs.release.outputs.image-matrix) }}' "$workflow" ||
    fail "release jobs do not use the validated component matrix"
grep -Fq "if: needs.release.outputs.image-count != '0'" "$workflow" ||
    fail "release workflow does not permit a release with no new images"

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
grep -Fq 'VERSION=${{ matrix.version }}' "$workflow" ||
    fail "image builds do not use the selected component version"

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
