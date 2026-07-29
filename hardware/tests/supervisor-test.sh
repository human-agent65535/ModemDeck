#!/usr/bin/env bash
set -Eeuo pipefail

readonly tests_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
readonly hardware_dir="$(cd -- "${tests_dir}/.." && pwd)"
readonly entrypoint="${hardware_dir}/bin/modemdeck-hardware-entrypoint"
test_root="$(mktemp -d)"

cleanup() {
  rm -rf "${test_root}"
}
trap cleanup EXIT

mkdir -p "${test_root}/bin"

cat > "${test_root}/bin/stay-alive" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
pid_file="${1:-}"
[[ -z "${pid_file}" ]] || printf '%s\n' "$$" > "${pid_file}"
trap 'exit 0' TERM INT
while true; do
  sleep 1 &
  wait $!
done
EOF

cat > "${test_root}/bin/fake-dbus" <<'EOF'
#!/usr/bin/env bash
exec "${FAKE_STAY_ALIVE}" "${DBUS_PID_FILE}"
EOF

cat > "${test_root}/bin/fake-dbus-send" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' '   boolean true'
EOF

cat > "${test_root}/bin/fake-setsid" <<'EOF'
#!/usr/bin/env bash
exec "$@"
EOF

cat > "${test_root}/bin/fake-mm" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf '%s\n' "$@" > "${MM_ARGS_FILE}"
if [[ "${MM_EXIT_CODE:-}" =~ ^[0-9]+$ ]]; then
  sleep 0.3
  exit "${MM_EXIT_CODE}"
fi
exec "${FAKE_STAY_ALIVE}" "${MM_PID_FILE}"
EOF

cat > "${test_root}/bin/fake-owner" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
case "${1:-}" in
  run)
    shift
    printf '%s\n' "$@" > "${OWNER_ARGS_FILE}"
    : > "${OWNER_READY_FILE}"
    if [[ "${OWNER_EXIT_CODE:-}" =~ ^[0-9]+$ ]]; then
      sleep 0.3
      exit "${OWNER_EXIT_CODE}"
    fi
    exec "${FAKE_STAY_ALIVE}" "${OWNER_PID_FILE}"
    ;;
  health)
    [[ -f "${OWNER_READY_FILE}" ]]
    ;;
  *)
    exit 2
    ;;
esac
EOF

cat > "${test_root}/bin/fake-agent" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
printf '%s\n' "$@" > "${AGENT_ARGS_FILE}"
if [[ "${AGENT_EXIT_CODE:-}" =~ ^[0-9]+$ ]]; then
  sleep 0.3
  exit "${AGENT_EXIT_CODE}"
fi
exec "${FAKE_STAY_ALIVE}" "${AGENT_PID_FILE}"
EOF

cat > "${test_root}/bin/fake-watchdog" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
if [[ " $* " == *" --cleanup-only "* ]]; then
  : > "${WATCHDOG_CLEANUP_FILE}"
  exit 0
fi
printf '%s\n' "$@" > "${WATCHDOG_ARGS_FILE}"
if [[ "${WATCHDOG_EXIT_CODE:-}" =~ ^[0-9]+$ ]]; then
  sleep 0.3
  exit "${WATCHDOG_EXIT_CODE}"
fi
exec "${FAKE_STAY_ALIVE}" "${WATCHDOG_PID_FILE}"
EOF

cat > "${test_root}/bin/fake-health" <<'EOF'
#!/usr/bin/env bash
set -Eeuo pipefail
[[ -s "${AGENT_ARGS_FILE}" ]]
EOF

chmod 0755 "${test_root}/bin/"*
touch "${test_root}/dbus.conf"
cat > "${test_root}/assignments.json" <<'EOF'
{
  "version": 1,
  "assignments": [
    {
      "id": "test-modem",
      "match": {"sysfs_path": "/sys/devices/test-modem"}
    }
  ]
}
EOF

common_env=(
  "MODEMDECK_DBUS_DAEMON_BIN=${test_root}/bin/fake-dbus"
  "MODEMDECK_DBUS_SEND_BIN=${test_root}/bin/fake-dbus-send"
  "MODEMDECK_MODEM_MANAGER_BIN=${test_root}/bin/fake-mm"
  "MODEMDECK_DEVICE_OWNER_BIN=${test_root}/bin/fake-owner"
  "MODEMDECK_AGENT_BIN=${test_root}/bin/fake-agent"
  "MODEMDECK_CALL_WATCHDOG_BIN=${test_root}/bin/fake-watchdog"
  "MODEMDECK_HEALTHCHECK_BIN=${test_root}/bin/fake-health"
  "MODEMDECK_SETSID_BIN=${test_root}/bin/fake-setsid"
  "MODEMDECK_DBUS_CONFIG=${test_root}/dbus.conf"
  "MODEMDECK_STARTUP_TIMEOUT_SECONDS=2"
  "MODEMDECK_SHUTDOWN_TIMEOUT_SECONDS=1"
  "MODEMDECK_AGENT_SOCKET_GID=$(id -g)"
  "FAKE_STAY_ALIVE=${test_root}/bin/stay-alive"
)

wait_for_file() {
  local file="$1"
  local attempt
  for attempt in {1..50}; do
    [[ -s "${file}" ]] && return
    sleep 0.05
  done
  printf 'supervisor-test: timed out waiting for %s\n' "${file}" >&2
  exit 1
}

assert_stopped() {
  local file="$1"
  local pid
  [[ -s "${file}" ]] || return 0
  pid="$(cat "${file}")"
  if kill -0 "${pid}" 2>/dev/null; then
    printf 'supervisor-test: child %s is still running\n' "${pid}" >&2
    exit 1
  fi
}

case_environment() {
  local case_dir="$1"
  printf '%s\n' \
    "MODEMDECK_RUNTIME_DIR=${case_dir}/run" \
    "MODEMDECK_MM_STATE_DIR=${case_dir}/mm-state" \
    "MODEMDECK_DBUS_SOCKET=${case_dir}/dbus/system_bus_socket" \
    "MM_ARGS_FILE=${case_dir}/mm.args" \
    "OWNER_ARGS_FILE=${case_dir}/owner.args" \
    "OWNER_READY_FILE=${case_dir}/owner.ready" \
    "AGENT_ARGS_FILE=${case_dir}/agent.args" \
    "WATCHDOG_ARGS_FILE=${case_dir}/watchdog.args" \
    "WATCHDOG_CLEANUP_FILE=${case_dir}/watchdog.cleanup" \
    "DBUS_PID_FILE=${case_dir}/dbus.pid" \
    "MM_PID_FILE=${case_dir}/mm.pid" \
    "OWNER_PID_FILE=${case_dir}/owner.pid" \
    "AGENT_PID_FILE=${case_dir}/agent.pid" \
    "WATCHDOG_PID_FILE=${case_dir}/watchdog.pid"
}

run_clean_shutdown_case() {
  local mode="$1"
  local case_dir="${test_root}/${mode}"
  local mode_args=("--mode" "${mode}")
  local case_env=()
  mkdir -p "${case_dir}"
  mapfile -t case_env < <(case_environment "${case_dir}")

  if [[ "${mode}" == "advanced" ]]; then
    mode_args+=("--assignments" "${test_root}/assignments.json")
  fi

  env \
    "${common_env[@]}" \
    "${case_env[@]}" \
    "${entrypoint}" "${mode_args[@]}" \
    >"${case_dir}/output.log" 2>&1 &
  local supervisor_pid=$!

  wait_for_file "${case_dir}/agent.args"
  wait_for_file "${case_dir}/watchdog.args"
  kill -TERM "${supervisor_pid}"
  wait "${supervisor_pid}"

  if [[ "${mode}" == "advanced" ]]; then
    grep -Fqx -- '--no-auto-scan' "${case_dir}/mm.args"
    grep -Fqx -- '--config' "${case_dir}/owner.args"
    grep -Fqx -- "${test_root}/assignments.json" "${case_dir}/owner.args"
  else
    if grep -Fq -- '--no-auto-scan' "${case_dir}/mm.args"; then
      printf '%s\n' "supervisor-test: simple mode disabled auto-scan" >&2
      exit 1
    fi
    [[ ! -e "${case_dir}/owner.args" ]] || {
      printf '%s\n' "supervisor-test: simple mode started device owner" >&2
      exit 1
    }
  fi

  grep -Fqx -- '--bearer-state-file' "${case_dir}/agent.args"
  grep -Fqx -- "${case_dir}/run/bearers.json" "${case_dir}/agent.args"
  grep -Fqx -- '--network-state-file' "${case_dir}/agent.args"
  grep -Fqx -- "${case_dir}/run/network.json" "${case_dir}/agent.args"
  grep -Fqx -- '--radio-state-file' "${case_dir}/agent.args"
  grep -Fqx -- "${case_dir}/run/radio-state.json" "${case_dir}/agent.args"
  grep -Fqx -- '--watchdog-heartbeat-file' "${case_dir}/agent.args"
  grep -Fqx -- "${case_dir}/run/agent-heartbeat.json" "${case_dir}/agent.args"
  grep -Fqx -- '--heartbeat-file' "${case_dir}/watchdog.args"
  grep -Fqx -- "${case_dir}/run/agent-heartbeat.json" "${case_dir}/watchdog.args"

  assert_stopped "${case_dir}/dbus.pid"
  assert_stopped "${case_dir}/mm.pid"
  assert_stopped "${case_dir}/owner.pid"
  assert_stopped "${case_dir}/agent.pid"
  assert_stopped "${case_dir}/watchdog.pid"
}

run_failure_case() {
  local component="$1"
  local expected_status="$2"
  local case_dir="${test_root}/failure-${component}"
  local case_env=()
  local failure_env=()
  local mode_args=("--mode" "simple")
  local status
  mkdir -p "${case_dir}"
  mapfile -t case_env < <(case_environment "${case_dir}")

  case "${component}" in
    mm)
      failure_env=("MM_EXIT_CODE=${expected_status}")
      ;;
    owner)
      failure_env=("OWNER_EXIT_CODE=${expected_status}")
      mode_args=(
        "--mode" "advanced"
        "--assignments" "${test_root}/assignments.json"
      )
      ;;
    agent)
      failure_env=("AGENT_EXIT_CODE=${expected_status}")
      ;;
    watchdog)
      failure_env=("WATCHDOG_EXIT_CODE=${expected_status}")
      ;;
    *)
      printf 'supervisor-test: unknown failure component %s\n' "${component}" >&2
      exit 1
      ;;
  esac

  set +e
  env \
    "${common_env[@]}" \
    "${case_env[@]}" \
    "${failure_env[@]}" \
    "${entrypoint}" "${mode_args[@]}" \
    >"${case_dir}/output.log" 2>&1
  status=$?
  set -e

  [[ "${status}" -eq "${expected_status}" ]] || {
    printf 'supervisor-test: expected status %s, got %s\n' \
      "${expected_status}" "${status}" >&2
    cat "${case_dir}/output.log" >&2
    exit 1
  }
  assert_stopped "${case_dir}/dbus.pid"
  assert_stopped "${case_dir}/mm.pid"
  assert_stopped "${case_dir}/owner.pid"
  assert_stopped "${case_dir}/agent.pid"
  assert_stopped "${case_dir}/watchdog.pid"
  if [[ "${component}" == "agent" ]]; then
    [[ -e "${case_dir}/watchdog.cleanup" ]] || {
      printf '%s\n' "supervisor-test: agent exit skipped emergency cleanup" >&2
      exit 1
    }
  fi
}

run_clean_shutdown_case simple
run_clean_shutdown_case advanced
run_failure_case mm 23
run_failure_case owner 24
run_failure_case agent 25
run_failure_case watchdog 26

printf '%s\n' "supervisor-test: ok"
