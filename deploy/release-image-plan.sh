#!/usr/bin/env bash

set -euo pipefail

release_tag=${1:?release tag is required}
vcs_ref=${2:?release commit is required}
previous_tag=${3:-}
namespace=${4:?GHCR namespace is required}
force_hardware=${5:-false}
manifest=deploy/release-manifest.json
stable_tag='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'

component_changed() {
    local component=$1
    [[ -n "$previous_tag" ]] || return 0

    local -a paths
    case "$component" in
        api)
            paths=(
                Dockerfile .dockerignore go.mod go.sum
                LICENSE NOTICE.md THIRD_PARTY_NOTICES.md
                scripts/docker-entrypoint.sh
                ':(glob)cmd/modemdeck/*.go'
                ':(glob)cmd/modemdeck/**/*.go'
                ':(glob)internal/**'
                ':(exclude,glob)internal/**/*_test.go'
                ':(exclude,glob)internal/ota/**'
                ':(exclude,glob)internal/**/.DS_Store'
            )
            ;;
        web)
            paths=(
                Dockerfile .dockerignore
                LICENSE NOTICE.md THIRD_PARTY_NOTICES.md
                scripts/nginx-entrypoint.sh
                web/index.html web/nginx.conf web/package.json web/package-lock.json
                web/tsconfig.json web/tsconfig.typescript7.json web/vite.config.ts
                ':(glob)web/public/**'
                ':(glob)web/src/**'
            )
            ;;
        updater)
            paths=(
                Dockerfile .dockerignore go.mod go.sum
                LICENSE NOTICE.md THIRD_PARTY_NOTICES.md
                ':(glob)cmd/modemdeck-updater/*.go'
                ':(glob)cmd/modemdeck-updater/**/*.go'
                ':(glob)internal/ota/**'
                ':(glob)internal/updatecheck/**'
                ':(exclude,glob)**/*_test.go'
            )
            ;;
        hardware)
            paths=(
                hardware/Dockerfile hardware/Dockerfile.dockerignore
                ':(glob)hardware/bin/**'
                ':(glob)hardware/dbus/**'
                ':(glob)hardware/ownership/**'
                ':(glob)agent/**'
                ':(exclude,glob)**/*_test.go'
                ':(exclude,glob)agent/README.md'
                ':(exclude,glob)hardware/ownership/**/.DS_Store'
                ':(exclude,glob)agent/**/.DS_Store'
            )
            ;;
        *)
            printf 'unknown release component: %s\n' "$component" >&2
            return 2
            ;;
    esac
    ! git diff --quiet "${previous_tag}^{commit}" "$vcs_ref" -- "${paths[@]}"
}

append_component() {
    local component=$1
    local mode=$2
    local dockerfile image target
    case "$component" in
        api)
            dockerfile=./Dockerfile
            image=modemdeck
            target=runtime
            ;;
        web)
            dockerfile=./Dockerfile
            image=modemdeck-web
            target=web-runtime
            ;;
        updater)
            dockerfile=./Dockerfile
            image=modemdeck-updater
            target=updater-runtime
            ;;
        hardware)
            dockerfile=./hardware/Dockerfile
            image=modemdeck-hardware
            target=runtime
            ;;
    esac
    matrix=$(jq -c \
        --arg component "$component" \
        --arg dockerfile "$dockerfile" \
        --arg image "$image" \
        --arg mode "$mode" \
        --arg source_version "$previous_tag" \
        --arg target "$target" \
        --arg version "$release_tag" \
        '.include += [{component: $component, mode: $mode, dockerfile: $dockerfile, image: $image, target: $target, version: $version, source_version: $source_version}]' \
        <<<"$matrix")
}

[[ "$release_tag" =~ $stable_tag ]] || {
    printf 'release tag must be a stable vX.Y.Z tag: %s\n' "$release_tag" >&2
    exit 1
}
[[ "$force_hardware" == true || "$force_hardware" == false ]] || {
    printf 'force_hardware must be true or false\n' >&2
    exit 1
}

matrix='{"include":[]}'
for component in api web updater hardware; do
    changed=false
    if component_changed "$component"; then
        changed=true
    fi
    if [[ "$component" == hardware && "$force_hardware" == true ]]; then
        changed=true
    fi

    if [[ "$changed" == true ]]; then
        append_component "$component" build
        continue
    fi
    [[ "$previous_tag" =~ $stable_tag ]] || {
        printf 'unchanged %s requires a previous stable release tag\n' "$component" >&2
        exit 1
    }
    append_component "$component" retain
done

for component in api web hardware updater; do
    expected="ghcr.io/${namespace}/modemdeck"
    [[ "$component" == web ]] && expected+="-web"
    [[ "$component" == hardware ]] && expected+="-hardware"
    [[ "$component" == updater ]] && expected+="-updater"
    actual=$(jq -r --arg component "$component" '.images[$component] // ""' "$manifest")
    [[ "$actual" == "$expected" ]] || {
        printf 'release manifest %s image must be %s\n' "$component" "$expected" >&2
        exit 1
    }
done

printf '%s\n' "$matrix"
