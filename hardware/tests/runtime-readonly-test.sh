#!/usr/bin/env bash
set -Eeuo pipefail

image="${1:-modemdeck-hardware:dev}"
name="modemdeck-hardware-readonly-$RANDOM-$$"
runtime_volume="${name}-runtime"
mm_volume="${name}-mm"

cleanup() {
  docker rm -f "${name}" >/dev/null 2>&1 || true
  docker volume rm "${runtime_volume}" >/dev/null 2>&1 || true
  docker volume rm "${mm_volume}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker volume create "${runtime_volume}" >/dev/null
docker volume create "${mm_volume}" >/dev/null

docker run --detach \
  --name "${name}" \
  --read-only \
  --tmpfs /run/dbus:rw,nosuid,nodev,noexec,mode=0770,size=16m \
  --mount "type=volume,source=${runtime_volume},target=/run/modemdeck" \
  --mount "type=volume,source=${mm_volume},target=/var/lib/ModemManager" \
  "${image}" \
  --mode simple >/dev/null

for _ in {1..100}; do
  state="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "${name}")"
  case "${state}" in
    healthy)
      docker exec "${name}" /usr/local/bin/modemdeck-hardware-healthcheck
      docker exec "${name}" test -S /run/modemdeck/agent.sock
      docker exec "${name}" test -s /run/modemdeck/hardware-mode
      docker exec "${name}" test -d /run/modemdeck/tmp
      docker exec "${name}" test -d /var/lib/ModemManager
      printf '%s\n' "runtime-readonly-test: ok"
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
printf '%s\n' "runtime-readonly-test: timed out waiting for healthy state" >&2
exit 1
