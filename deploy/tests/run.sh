#!/bin/sh

set -eu

tests_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)

"${tests_dir}/static-test.sh"
"${tests_dir}/release-workflow-test.sh"
"${tests_dir}/release-image-plan-test.sh"
"${tests_dir}/install-check-test.sh"
"${tests_dir}/install-behavior-test.sh"

printf '%s\n' "deployment-tests: ok"
