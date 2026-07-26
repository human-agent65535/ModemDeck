#!/usr/bin/env bash
set -Eeuo pipefail

readonly tests_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly hardware_dir="$(cd -- "${tests_dir}/.." && pwd)"

fail() {
  printf 'static-test: %s\n' "$*" >&2
  exit 1
}

bash -n "${hardware_dir}/bin/modemdeck-hardware-entrypoint"
bash -n "${hardware_dir}/bin/modemdeck-hardware-healthcheck"
bash -n "${hardware_dir}/build-image.sh"
bash -n "${hardware_dir}/tests/verify-oci-platforms.sh"
bash -n "${hardware_dir}/tests/runtime-readonly-test.sh"
bash -n "${hardware_dir}/tests/runtime-advanced-readonly-test.sh"

grep -Fq \
  'debian:trixie-slim@sha256:020c0d20b9880058cbe785a9db107156c3c75c2ac944a6aa7ab59f2add76a7bd' \
  "${hardware_dir}/Dockerfile" \
  || fail "runtime is not pinned to the reviewed Debian trixie image"
grep -Fq 'MODEMMANAGER_DEBIAN_VERSION=1.24.0-1+deb13u1' \
  "${hardware_dir}/Dockerfile" \
  || fail "ModemManager source version is not pinned to Debian 13 stable/security"
if grep -Eq 'bookworm|1\.20\.4' "${hardware_dir}/Dockerfile"; then
  fail "obsolete bookworm or ModemManager 1.20 baseline remains"
fi
grep -Fq -- '-Dudev=true' "${hardware_dir}/Dockerfile" \
  || fail "ModemManager libudev support is not explicit"
grep -Fq -- '-Dat_command_via_dbus=true' "${hardware_dir}/Dockerfile" \
  || fail "ModemManager AT D-Bus build option is missing"
grep -Fq -- '-Dpolkit=no' "${hardware_dir}/Dockerfile" \
  || fail "ModemManager Polkit disable option is missing"
grep -Fq 'libqmi-glib5)" ge 1.36' "${hardware_dir}/Dockerfile" \
  || fail "libqmi minimum version assertion is missing"
grep -Fq 'libmbim-glib4)" ge 1.32' "${hardware_dir}/Dockerfile" \
  || fail "libmbim minimum version assertion is missing"
grep -Fq 'tests/verify-oci-platforms.sh' "${hardware_dir}/build-image.sh" \
  || fail "OCI builds do not verify their exported platforms"

grep -Fq -- '--no-auto-scan' \
  "${hardware_dir}/bin/modemdeck-hardware-entrypoint" \
  || fail "advanced mode does not disable automatic scanning"
grep -Fq 'modemdeck-device-owner' \
  "${hardware_dir}/bin/modemdeck-hardware-entrypoint" \
  || fail "advanced device owner is not supervised"
if grep -Fq -- '--initial-kernel-events' \
  "${hardware_dir}/bin/modemdeck-hardware-entrypoint"; then
  fail "raw initial kernel event injection bypasses explicit ownership"
fi
grep -Fq -- '--bearer-state-file' \
  "${hardware_dir}/bin/modemdeck-hardware-entrypoint" \
  || fail "Agent bearer ownership file is not explicit"
grep -Fq -- '--network-state-file' \
  "${hardware_dir}/bin/modemdeck-hardware-entrypoint" \
  || fail "Agent network ownership file is not explicit"

grep -Fq -- '--unix-socket' \
  "${hardware_dir}/bin/modemdeck-hardware-healthcheck" \
  || fail "healthcheck does not perform an HTTP request over the Agent socket"
grep -Fq 'http://localhost/v1/health' \
  "${hardware_dir}/bin/modemdeck-hardware-healthcheck" \
  || fail "healthcheck does not request /v1/health"
grep -Fq '.provider.available == true' \
  "${hardware_dir}/bin/modemdeck-hardware-healthcheck" \
  || fail "healthcheck does not require provider availability"
grep -Fq '.provider.boot_epoch' \
  "${hardware_dir}/bin/modemdeck-hardware-healthcheck" \
  || fail "healthcheck does not require a provider boot epoch"

grep -Fq 'VOLUME ["/var/lib/ModemManager", "/run/modemdeck"]' \
  "${hardware_dir}/Dockerfile" \
  || fail "persistent ModemManager and Agent ownership volumes are missing"
grep -Fq 'TMPDIR=/run/modemdeck/tmp' "${hardware_dir}/Dockerfile" \
  || fail "temporary writes are not contained in /run/modemdeck"

grep -Fq '<deny own="*"/>' "${hardware_dir}/dbus/modemdeck-system.conf" \
  || fail "private bus does not deny arbitrary name ownership"
grep -Fq '<deny send_type="method_call"/>' \
  "${hardware_dir}/dbus/modemdeck-system.conf" \
  || fail "private bus does not deny arbitrary method calls"
grep -Fq '<allow own="org.freedesktop.ModemManager1"/>' \
  "${hardware_dir}/dbus/modemdeck-system.conf" \
  || fail "private bus does not allow the ModemManager name"
if grep -Eq '<(standard_system_servicedirs|servicedir)>' \
  "${hardware_dir}/dbus/modemdeck-system.conf"; then
  fail "private bus unexpectedly enables service activation"
fi

if grep -Eq \
  '(^|[[:space:]/])(systemctl|systemd|udevd|NetworkManager|polkitd)([[:space:]]|$)' \
  "${hardware_dir}/bin/"*; then
  fail "runtime scripts invoke a forbidden host service"
fi

grep -Fq 'wait -n -p exited_pid' \
  "${hardware_dir}/bin/modemdeck-hardware-entrypoint" \
  || fail "PID 1 does not wait for the first failed core process"

grep -Fq 'NameHasOwner' "${hardware_dir}/ownership/guard.go" \
  || fail "advanced host ModemManager ownership check is missing"
grep -Fq 'GetManagedObjects' "${hardware_dir}/ownership/guard.go" \
  || fail "advanced host ModemManager object check is missing"
[[ "$(grep -Fc 'CallWithContext(' "${hardware_dir}/ownership/guard.go")" -eq 2 ]] \
  || fail "advanced host guard must limit D-Bus calls to NameHasOwner and GetManagedObjects"
if grep -Eq \
  '\.(Inhibit|Delete|Enable|Disable|Scan)|DeleteBearer|SetPowerState' \
  "${hardware_dir}/ownership/guard.go"; then
  fail "advanced host guard contains a host-mutating ModemManager call"
fi
if grep -Eq 'syscall\.(Open|Creat)|os\.OpenFile' \
  "${hardware_dir}/ownership/discovery.go"; then
  fail "advanced readiness check opens an assigned device instead of observing it"
fi
if grep -REq \
  '(^|[^[:alnum:]_])(systemctl|udevadm|modprobe|rmmod|pkill|killall)([^[:alnum:]_]|$)' \
  "${hardware_dir}/ownership" "${hardware_dir}/bin"; then
  fail "hardware runtime contains an external service or device-policy control command"
fi

printf '%s\n' "static-test: ok"
