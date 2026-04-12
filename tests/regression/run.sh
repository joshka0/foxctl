#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

mapfile -t CASES < <(find "${ROOT_DIR}" -mindepth 1 -maxdepth 1 -type d \
  -not -name "_*" \
  -not -name "fixtures" \
  -not -name "node_modules" \
  | sort)

if [[ ${#CASES[@]} -eq 0 ]]; then
  echo "No regression cases found under: ${ROOT_DIR}"
  exit 0
fi

echo "Running ${#CASES[@]} regression case(s)..."

for case_dir in "${CASES[@]}"; do
  run="${case_dir}/run.sh"
  if [[ ! -f "${run}" ]]; then
    echo "ERROR: Missing run.sh in ${case_dir}"
    exit 1
  fi

  echo
  echo "==> $(basename "${case_dir}")"
  (cd "${case_dir}" && bash "./run.sh")
done

echo
echo "All regression cases passed."
