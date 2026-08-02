#!/bin/sh

set -eu

tests_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
source_repo=$(CDPATH='' cd -- "${tests_dir}/../.." && pwd)
fixture=$(mktemp -d)

cleanup() {
    rm -rf -- "$fixture"
}
trap cleanup EXIT HUP INT TERM

fail() {
    printf 'release-image-plan-test: %s\n' "$*" >&2
    exit 1
}

mkdir -p \
    "${fixture}/deploy" \
    "${fixture}/web/src" \
    "${fixture}/cmd/modemdeck" \
    "${fixture}/cmd/modemdeck-updater" \
    "${fixture}/internal/ota" \
    "${fixture}/agent/cmd/modemdeck-agent" \
    "${fixture}/hardware/bin"
cp "${source_repo}/deploy/release-image-plan.sh" "${fixture}/deploy/release-image-plan.sh"
chmod 0755 "${fixture}/deploy/release-image-plan.sh"

write_manifest() {
    {
        printf '%s\n' '{' '  "schema_version": 2,' '  "images": {'
        printf '%s\n' \
            '    "api": "ghcr.io/human-agent65535/modemdeck",' \
            '    "web": "ghcr.io/human-agent65535/modemdeck-web",' \
            '    "hardware": "ghcr.io/human-agent65535/modemdeck-hardware",' \
            '    "updater": "ghcr.io/human-agent65535/modemdeck-updater"' \
            '  }'
        printf '%s\n' '}'
    } >"${fixture}/deploy/release-manifest.json"
}

git -C "$fixture" init --quiet
git -C "$fixture" config user.name ModemDeck
git -C "$fixture" config user.email modemdeck@example.invalid
printf '%s\n' base >"${fixture}/Dockerfile"
printf '%s\n' base >"${fixture}/web/src/main.ts"
printf '%s\n' base >"${fixture}/cmd/modemdeck/main.go"
printf '%s\n' base >"${fixture}/cmd/modemdeck-updater/main.go"
printf '%s\n' base >"${fixture}/internal/ota/controller.go"
printf '%s\n' base >"${fixture}/agent/cmd/modemdeck-agent/main.go"
printf '%s\n' base >"${fixture}/hardware/bin/entrypoint"
write_manifest
git -C "$fixture" add .
git -C "$fixture" commit --quiet -m v1.9.5
git -C "$fixture" tag -a v1.9.3 -m v1.9.3
git -C "$fixture" tag -a v1.9.5 -m v1.9.5

printf '%s\n' changed >"${fixture}/Dockerfile"
write_manifest
git -C "$fixture" add .
git -C "$fixture" commit --quiet -m v1.9.6
git -C "$fixture" tag -a v1.9.6 -m v1.9.6
plan=$(cd "$fixture" && deploy/release-image-plan.sh \
    v1.9.6 HEAD v1.9.5 human-agent65535 false)
[ "$(printf '%s' "$plan" | jq -r '.include | map(select(.mode == "build") | .component) | join(",")')" \
    = 'api,web,updater' ] ||
    fail "shared Dockerfile change did not select exactly API, Web, and Updater: $plan"
[ "$(printf '%s' "$plan" | jq -r '.include | map(select(.mode == "retain") | .component) | join(",")')" \
    = hardware ] || fail "unchanged Hardware was not retained: $plan"

printf '%s\n' changed-again >"${fixture}/web/src/main.ts"
write_manifest
git -C "$fixture" add .
git -C "$fixture" commit --quiet -m v1.9.7
git -C "$fixture" tag -a v1.9.7 -m v1.9.7
plan=$(cd "$fixture" && deploy/release-image-plan.sh \
    v1.9.7 HEAD v1.9.6 human-agent65535 false)
[ "$(printf '%s' "$plan" | jq -r '.include | map(select(.mode == "build") | .component) | join(",")')" = web ] ||
    fail "Web-only change selected another container: $plan"
[ "$(printf '%s' "$plan" | jq -r '.include | map(select(.mode == "retain") | .source_version) | unique | join(",")')" \
    = v1.9.6 ] || fail "retained images did not use the previous release: $plan"

printf '%s\n' test-only >"${fixture}/agent/cmd/modemdeck-agent/main_test.go"
git -C "$fixture" add .
git -C "$fixture" commit --quiet -m v1.9.8
git -C "$fixture" tag -a v1.9.8 -m v1.9.8
plan=$(cd "$fixture" && deploy/release-image-plan.sh \
    v1.9.8 HEAD v1.9.7 human-agent65535 false)
[ "$(printf '%s' "$plan" | jq -r '.include | map(select(.mode == "build")) | length')" = 0 ] ||
    fail "test-only release rebuilt a runtime image: $plan"
[ "$(printf '%s' "$plan" | jq -r '.include | map(select(.mode == "retain")) | length')" = 4 ] ||
    fail "test-only release did not retain all runtime images: $plan"

printf '%s\n' runtime-change >"${fixture}/agent/cmd/modemdeck-agent/main.go"
write_manifest
git -C "$fixture" add .
git -C "$fixture" commit --quiet -m v1.9.9
git -C "$fixture" tag -a v1.9.9 -m v1.9.9
plan=$(cd "$fixture" && deploy/release-image-plan.sh \
    v1.9.9 HEAD v1.9.8 human-agent65535 false)
[ "$(printf '%s' "$plan" | jq -r '.include | map(select(.mode == "build") | .component) | join(",")')" = hardware ] ||
    fail "Hardware runtime change selected another container: $plan"

printf '%s\n' 'release-image-plan-test: ok'
