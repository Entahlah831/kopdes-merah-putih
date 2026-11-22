#!/usr/bin/env bash
# Reproducible container/SAST/contract scans (Linux / CI with Docker):
# hadolint, trivy (config + image), semgrep, and schemathesis against LOCAL Set A/B only.
set -euo pipefail

ENV_FILE="${ENV_FILE:-.env.test}"
if [ -f "${ENV_FILE}" ]; then
  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a
fi

API_HOST_PORT="${API_HOST_PORT:?API_HOST_PORT wajib disetel di ${ENV_FILE}}"
OUT_DIR="${OUT_DIR:-reports/baseline}"
mkdir -p "${OUT_DIR}"

echo "[1/4] Hadolint Dockerfile"
docker run --rm -i hadolint/hadolint < Dockerfile | tee "${OUT_DIR}/hadolint.txt"

echo "[2/4] Trivy IaC/Config scan"
trivy config --severity MEDIUM,HIGH,CRITICAL --format json --output "${OUT_DIR}/trivy_config.json" .

echo "[3/4] Semgrep Go + OWASP + custom Kopdes ruleset"
semgrep scan --config auto --config tests/security/semgrep-kopdes.yml --json --output "${OUT_DIR}/semgrep.json" .

echo "[4/4] Schemathesis OpenAPI fuzz against http://127.0.0.1:${API_HOST_PORT}"
schemathesis run docs/openapi.yaml \
  --url "http://127.0.0.1:${API_HOST_PORT}" \
  --checks not_a_server_error,status_code_conformance,content_type_conformance \
  --report "${OUT_DIR}/schemathesis_report.json"
