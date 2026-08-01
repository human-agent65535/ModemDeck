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
    api_version=$1
    web_version=$2
    hardware_version=$3
    updater_version=$4
    {
        printf '%s\n' '{' '  "schema_version": 1,' '  "images": {'
        printf '%s\n' \
            '    "api": "ghcr.io/human-agent65535/modemdeck",' \
            '    "web": "ghcr.io/human-agent65535/modemdeck-web",' \
            '    "hardware": "ghcr.io/human-agent65535/modemdeck-hardware",' \
            '    "updater": "ghcr.io/human-agent65535/modemdeck-updater"' \
            '  },'
        printf '  "api_version": "%s",\n' "$api_version"
        printf '  "web_version": "%s",\n' "$web_version"
        printf '  "hardware_version": "%s",\n' "$hardware_version"
        printf '  "updater_version": "%s"\n' "$updater_version"
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
write_manifest v1.9.5 v1.9.5 v1.9.3 v1.9.5
# v1.9.5 predates per-component versions except for Hardware.
sed -i.bak '/"api_version"/d; /"web_version"/d; /"updater_version"/d' \
    "${fixture}/deploy/release-manifest.json"
sed -i.bak 's/"hardware_version": "v1.9.3",/"hardware_version": "v1.9.3"/' \
    "${fixture}/deploy/release-manifest.json"
rm -f "${fixture}/deploy/release-manifest.json.bak"
git -C "$fixture" add .
git -C "$fixture" commit --quiet -m v1.9.5
git -C "$fixture" tag -a v1.9.3 -m v1.9.3
git -C "$fixture" tag -a v1.9.5 -m v1.9.5

printf '%s\n' changed >"${fixture}/Dockerfile"
write_manifest v1.9.6 v1.9.6 v1.9.3 v1.9.6
git -C "$fixture" add .
git -C "$fixture" commit --quiet -m v1.9.6
git -C "$fixture" tag -a v1.9.6 -m v1.9.6
plan=$(cd "$fixture" && deploy/release-image-plan.sh \
    v1.9.6 HEAD v1.9.5 human-agent65535 false)
[ "$(printf '%s' "$plan" | jq -r '.include | map(.component) | join(",")')" \
    = 'api,web,updater' ] ||
    fail "shared Dockerfile change did not select exactly API, Web, and Updater: $plan"

printf '%s\n' changed-again >"${fixture}/web/src/main.ts"
write_manifest v1.9.6 v1.9.7 v1.9.3 v1.9.6
git -C "$fixture" add .
git -C "$fixture" commit --quiet -m v1.9.7
git -C "$fixture" tag -a v1.9.7 -m v1.9.7
plan=$(cd "$fixture" && deploy/release-image-plan.sh \
    v1.9.7 HEAD v1.9.6 human-agent65535 false)
[ "$(printf '%s' "$plan" | jq -r '.include | map(.component) | join(",")')" = web ] ||
    fail "Web-only change selected another container: $plan"

write_manifest v1.9.6 v1.9.7 v1.9.3 v1.9.7
if (cd "$fixture" && deploy/release-image-plan.sh \
    v1.9.7 HEAD v1.9.6 human-agent65535 false >/dev/null 2>&1)
then
    fail "unchanged Updater was allowed to advance its component version"
fi
write_manifest v1.9.6 v1.9.7 v1.9.3 v1.9.6

printf '%s\n' test-only >"${fixture}/agent/cmd/modemdeck-agent/main_test.go"
git -C "$fixture" add .
git -C "$fixture" commit --quiet -m v1.9.8
git -C "$fixture" tag -a v1.9.8 -m v1.9.8
plan=$(cd "$fixture" && deploy/release-image-plan.sh \
    v1.9.8 HEAD v1.9.7 human-agent65535 false)
[ "$(printf '%s' "$plan" | jq -r '.include | length')" = 0 ] ||
    fail "test-only release selected a runtime image: $plan"

printf '%s\n' runtime-change >"${fixture}/agent/cmd/modemdeck-agent/main.go"
write_manifest v1.9.6 v1.9.7 v1.9.9 v1.9.6
git -C "$fixture" add .
git -C "$fixture" commit --quiet -m v1.9.9
git -C "$fixture" tag -a v1.9.9 -m v1.9.9
plan=$(cd "$fixture" && deploy/release-image-plan.sh \
    v1.9.9 HEAD v1.9.8 human-agent65535 false)
[ "$(printf '%s' "$plan" | jq -r '.include | map(.component) | join(",")')" = hardware ] ||
    fail "Hardware runtime change selected another container: $plan"

printf '%s\n' 'release-image-plan-test: ok'
