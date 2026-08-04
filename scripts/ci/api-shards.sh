#!/usr/bin/env bash
# Run internal/api tests in isolated Go processes and merge their coverage.
set -euo pipefail

if [ "$#" -ne 2 ]; then
  printf 'usage: %s PACKAGE MERGED_COVERPROFILE\n' "$0" >&2
  exit 2
fi

package="$1"
merged_profile="$2"
: "${CI_ARTIFACT_DIR:=${PWD}/.artifacts/ci}"
: "${CI_SCRIPTS_DIR:=${PWD}/scripts/ci}"
: "${CI_API_SHARDS:=4}"
: "${CI_API_SHARD_GOMAXPROCS:=1}"
: "${CI_API_SHARD_TIMEOUT:=30m}"
shard_dir="${CI_API_SHARD_DIR:-${CI_ARTIFACT_DIR}/api-shards}"

case "${CI_API_SHARDS}" in
  ''|*[!0-9]*) printf 'CI_API_SHARDS must be a positive integer\n' >&2; exit 2 ;;
esac
case "${CI_API_SHARD_GOMAXPROCS}" in
  ''|*[!0-9]*) printf 'CI_API_SHARD_GOMAXPROCS must be a positive integer\n' >&2; exit 2 ;;
esac
if [ "${CI_API_SHARDS}" -lt 2 ] || [ "${CI_API_SHARD_GOMAXPROCS}" -lt 1 ]; then
  printf 'CI_API_SHARDS must be >= 2 and CI_API_SHARD_GOMAXPROCS >= 1\n' >&2
  exit 2
fi

mkdir -p "${shard_dir}"
rm -f "${shard_dir}"/test-list.txt "${shard_dir}"/expected-roots.txt \
  "${shard_dir}"/shard-*.regex "${shard_dir}"/shard-*.json \
  "${shard_dir}"/shard-*.log "${shard_dir}"/shard-*.rc \
  "${shard_dir}"/serial.regex "${shard_dir}"/serial.json \
  "${shard_dir}"/serial.log "${shard_dir}"/serial.rc \
  "${shard_dir}"/coverage-*.out

list_file="${shard_dir}/test-list.txt"
go test "${package}" -short -run '^$' -list '^Test' > "${list_file}"
mapfile -t tests < <(LC_ALL=C sort -u "${list_file}" | awk '/^Test[A-Za-z0-9_]+$/{print}')
if [ "${#tests[@]}" -eq 0 ]; then
  printf 'no top-level tests found for %s\n' "${package}" >&2
  exit 1
fi
printf '%s\n' "${tests[@]}" > "${shard_dir}/expected-roots.txt"

declare -A serial_set=()
# This root passed serially but failed under concurrent process scheduling;
# keep its coverage and assertions while avoiding an unsafe parallel lane.
# Treat an explicitly empty override like an unset override: CI wrappers often
# export optional variables as empty, and that must not silently disable fencing.
serial_csv="${CI_API_SERIAL_ROOTS:-TestRollbackPreservesRuntimeIdentityAndProtocolConfigBytes}"
if [ -n "${serial_csv}" ]; then
  IFS=',' read -r -a requested_serial <<< "${serial_csv}"
  for serial_root in "${requested_serial[@]}"; do
    if [[ ! "${serial_root}" =~ ^Test[A-Za-z0-9_]+$ ]]; then
      printf 'invalid CI_API_SERIAL_ROOTS entry: %s\n' "${serial_root}" >&2
      exit 2
    fi
    found=0
    for test_name in "${tests[@]}"; do
      if [ "${test_name}" = "${serial_root}" ]; then
        found=1
        break
      fi
    done
    if [ "${found}" -eq 0 ]; then
      printf 'CI_API_SERIAL_ROOTS entry is not a listed test: %s\n' "${serial_root}" >&2
      exit 2
    fi
    serial_set["${serial_root}"]=1
  done
fi

parallel_tests=()
serial_tests=()
for test_name in "${tests[@]}"; do
  if [[ -n "${serial_set[${test_name}]:-}" ]]; then
    serial_tests+=("${test_name}")
  else
    parallel_tests+=("${test_name}")
  fi
done
if [ "${#parallel_tests[@]}" -eq 0 ]; then
  printf 'serial policy removed every test from concurrent API shards\n' >&2
  exit 2
fi

shard_count="${CI_API_SHARDS}"
if [ "${#parallel_tests[@]}" -lt "${shard_count}" ]; then
  shard_count="${#parallel_tests[@]}"
fi
for ((i = 1; i <= shard_count; i++)); do
  printf '^(' > "${shard_dir}/shard-${i}.regex"
done
declare -a shard_seen=()
for ((i = 1; i <= shard_count; i++)); do
  shard_seen[i]=0
done
for ((index = 0; index < ${#parallel_tests[@]}; index++)); do
  shard=$((index % shard_count + 1))
  if [ "${shard_seen[shard]}" -ne 0 ]; then
    printf '|' >> "${shard_dir}/shard-${shard}.regex"
  fi
  printf '%s' "${parallel_tests[index]}" >> "${shard_dir}/shard-${shard}.regex"
  shard_seen[shard]=1
done
for ((i = 1; i <= shard_count; i++)); do
  printf ')$\n' >> "${shard_dir}/shard-${i}.regex"
done

serial_count="${#serial_tests[@]}"
if [ "${serial_count}" -gt 0 ]; then
  printf '^(' > "${shard_dir}/serial.regex"
  for ((index = 0; index < serial_count; index++)); do
    if [ "${index}" -gt 0 ]; then
      printf '|' >> "${shard_dir}/serial.regex"
    fi
    printf '%s' "${serial_tests[index]}" >> "${shard_dir}/serial.regex"
  done
  printf ')$\n' >> "${shard_dir}/serial.regex"
fi

pids=()
cleanup() {
  local pid
  for pid in "${pids[@]:-}"; do
    kill "${pid}" 2>/dev/null || true
  done
}
trap cleanup INT TERM
for ((i = 1; i <= shard_count; i++)); do
  (
    set +e
    GOMAXPROCS="${CI_API_SHARD_GOMAXPROCS}" \
      go test "${package}" -race -count=1 -timeout "${CI_API_SHARD_TIMEOUT}" \
      -json -run "$(cat "${shard_dir}/shard-${i}.regex")" \
      -coverprofile="${shard_dir}/coverage-${i}.out" \
      > "${shard_dir}/shard-${i}.json" 2>&1
    rc=$?
    printf '%s\n' "${rc}" > "${shard_dir}/shard-${i}.rc"
    exit "${rc}"
  ) > "${shard_dir}/shard-${i}.log" 2>&1 &
  pids+=("$!")
done

run_rc=0
for pid in "${pids[@]}"; do
  wait "${pid}" || run_rc=1
done
pids=()
trap - INT TERM

for ((i = 1; i <= shard_count; i++)); do
  cat "${shard_dir}/shard-${i}.json"
done
if [ "${run_rc}" -ne 0 ]; then
  printf 'API concurrent shard test failed; shard logs are in %s\n' "${shard_dir}" >&2
  exit "${run_rc}"
fi

serial_rc=0
if [ "${serial_count}" -gt 0 ]; then
  set +e
  GOMAXPROCS="${CI_API_SHARD_GOMAXPROCS}" \
    go test "${package}" -race -count=1 -timeout "${CI_API_SHARD_TIMEOUT}" \
    -json -run "$(cat "${shard_dir}/serial.regex")" \
    -coverprofile="${shard_dir}/coverage-serial.out" \
    > "${shard_dir}/serial.json" 2>&1
  serial_rc=$?
  set -e
  printf '%s\n' "${serial_rc}" > "${shard_dir}/serial.rc"
  cat "${shard_dir}/serial.json"
  if [ "${serial_rc}" -ne 0 ]; then
    printf 'API serial safety lane failed; log is in %s\n' "${shard_dir}" >&2
    exit "${serial_rc}"
  fi
fi

verification_logs=("${shard_dir}"/shard-*.json)
if [ "${serial_count}" -gt 0 ]; then
  verification_logs+=("${shard_dir}/serial.json")
fi
python3 "${CI_SCRIPTS_DIR}/verify-test-shards.py" \
  "${shard_dir}/expected-roots.txt" \
  "${verification_logs[@]}"

coverage_inputs=("${shard_dir}"/coverage-*.out)
expected_profiles=$((shard_count + serial_count))
if [ "${#coverage_inputs[@]}" -ne "${expected_profiles}" ]; then
  printf 'expected %s API coverage profiles, found %s\n' "${expected_profiles}" "${#coverage_inputs[@]}" >&2
  exit 1
fi
python3 "${CI_SCRIPTS_DIR}/merge-coverprofiles.py" "${merged_profile}" "${coverage_inputs[@]}"
