#!/usr/bin/env bash
set -Eeuo pipefail

image="${1:-modemdeck-hardware:dev}"
external_bus_image="${MODEMDECK_TEST_EXTERNAL_BUS_IMAGE:-${image}}"
suffix="$RANDOM-$$"
name="modemdeck-hardware-advanced-${suffix}"
bus_name="modemdeck-external-bus-${suffix}"
runtime_volume="${name}-runtime"
mm_volume="${name}-mm"
external_bus_volume="${name}-external-bus"
test_root="$(mktemp -d)"

cleanup() {
  docker rm -f "${name}" "${bus_name}" >/dev/null 2>&1 || true
  docker volume rm \
    "${runtime_volume}" \
    "${mm_volume}" \
    "${external_bus_volume}" >/dev/null 2>&1 || true
  rm -rf "${test_root}"
}
trap cleanup EXIT

mkdir -p "${test_root}/udev/data"
cat > "${test_root}/assignments.json" <<'EOF'
{
  "version": 1,
  "poll_interval_ms": 250,
  "settle_ms": 0,
  "port_ready_timeout_ms": 1000,
  "host": {
    "proc_root": "/run/host-proc",
    "system_bus_address": "unix:path=/run/host-dbus/system_bus_socket"
  },
  "assignments": [
    {
      "id": "optional-absent-modem",
      "required_at_startup": false,
      "match": {
        "sysfs_path": "/sys/devices/modemdeck-test-device-that-does-not-exist"
      }
    }
  ]
}
EOF

docker volume create "${runtime_volume}" >/dev/null
docker volume create "${mm_volume}" >/dev/null
docker volume create "${external_bus_volume}" >/dev/null

docker run --detach \
  --name "${bus_name}" \
  --read-only \
  --mount "type=volume,source=${external_bus_volume},target=/run/dbus" \
  --entrypoint /usr/bin/dbus-daemon \
  "${external_bus_image}" \
  --nofork \
  --nopidfile \
  --config-file=/etc/dbus-1/modemdeck-system.conf >/dev/null

for _ in {1..50}; do
  docker exec "${bus_name}" test -S /run/dbus/system_bus_socket 2>/dev/null \
    && break
  sleep 0.1
done
docker exec "${bus_name}" test -S /run/dbus/system_bus_socket

docker run --detach \
  --name "${name}" \
  --read-only \
  --cap-add SYS_PTRACE \
  --tmpfs /run/dbus:rw,nosuid,nodev,noexec,mode=0770,size=16m \
  --mount "type=volume,source=${runtime_volume},target=/run/modemdeck" \
  --mount "type=volume,source=${mm_volume},target=/var/lib/ModemManager" \
  --mount "type=volume,source=${external_bus_volume},target=/run/host-dbus,readonly" \
  --mount "type=bind,source=/proc,target=/run/host-proc,readonly" \
  --mount "type=bind,source=${test_root}/udev,target=/run/udev,readonly" \
  --mount "type=bind,source=${test_root}/assignments.json,target=/etc/modemdeck/device-assignments.json,readonly" \
  "${image}" \
  --mode advanced \
  --assignments /etc/modemdeck/device-assignments.json >/dev/null

for _ in {1..100}; do
  state="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "${name}")"
  case "${state}" in
    healthy)
      docker exec "${name}" /usr/local/bin/modemdeck-hardware-healthcheck
      docker exec "${name}" \
        /usr/local/bin/modemdeck-device-owner health \
        --status-file /run/modemdeck/device-owner-status.json \
        --max-age-seconds 10
      docker exec "${name}" grep -Fqx advanced /run/modemdeck/hardware-mode
      docker exec "${name}" jq -e '
        .ready == true
        and (.assignments | length == 1)
        and .assignments[0].id == "optional-absent-modem"
        and .assignments[0].state == "absent"
      ' /run/modemdeck/device-owner-status.json >/dev/null
      docker top "${name}" -eo pid,comm,args | awk '
        $2 == "ModemManager" {
          found = 1
          for (field = 3; field <= NF; field++) {
            if ($field == "--no-auto-scan") {
              no_auto_scan = 1
            }
          }
        }
        END {
          exit !(found && no_auto_scan)
        }
      '
      printf '%s\n' "runtime-advanced-readonly-test: ok"
      exit 0
      ;;
    unhealthy)
      docker logs "${name}" >&2
      exit 1
      ;;
  esac
  sleep 0.2
done

docker logs "${name}" >&2
printf '%s\n' "runtime-advanced-readonly-test: timed out waiting for healthy state" >&2
exit 1
