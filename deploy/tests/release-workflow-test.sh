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

matrix_has() {
    component=$1
    dockerfile=$2
    image=$3
    target=$4

    awk \
        -v expected_component="$component" \
        -v expected_dockerfile="$dockerfile" \
        -v expected_image="$image" \
        -v expected_target="$target" '
        $0 == "          - component: " expected_component {
            found_component = 1
            in_component = 1
            next
        }
        in_component && /^          - component:/ { in_component = 0 }
        in_component && $0 == "            dockerfile: " expected_dockerfile {
            found_dockerfile = 1
        }
        in_component && $0 == "            image: " expected_image {
            found_image = 1
        }
        in_component && $0 == "            target: " expected_target {
            found_target = 1
        }
        END {
            exit !(found_component && found_dockerfile && found_image && found_target)
        }
    ' "$workflow"
}

matrix_has api ./Dockerfile modemdeck runtime ||
    fail "API release image contract is incomplete"
matrix_has web ./Dockerfile modemdeck-web web-runtime ||
    fail "Web release image contract is incomplete"
matrix_has hardware ./hardware/Dockerfile modemdeck-hardware runtime ||
    fail "Hardware release image contract is incomplete"

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
