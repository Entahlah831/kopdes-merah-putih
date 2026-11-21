#!/usr/bin/env bash
# Reproducible OWASP ZAP Baseline + OpenAPI API Scan against LOCAL Set A/Set B stack only.
# Reads API_HOST_PORT from ENV_FILE (.env.test / .env.test.alt). Never targets remote hosts.
set -euo pipefail

ENV_FILE="${ENV_FILE:-.env.test}"
if [ -f "${ENV_FILE}" ]; then
  set -a
  # shellcheck disable=SC1090
  source "${ENV_FILE}"
  set +a
fi

API_HOST_PORT="${API_HOST_PORT:?API_HOST_PORT wajib disetel di ${ENV_FILE}}"
TARGET_URL="http://127.0.0.1:${API_HOST_PORT}"
OUT_DIR="${OUT_DIR:-reports/baseline}"
mkdir -p "${OUT_DIR}"

echo "[ZAP] Menjalankan baseline & OpenAPI scan terhadap stack lokal ${TARGET_URL}"
docker run --rm --network host \
  -v "$(pwd)/docs:/zap/wrk/docs:ro" \
  -v "$(pwd)/${OUT_DIR}:/zap/wrk/out" \
  ghcr.io/zaproxy/zaproxy:stable \
  zap-api-scan.py \
  -t "${TARGET_URL}/api/v1/public/config" \
  -f openapi \
  -O /zap/wrk/docs/openapi.yaml \
  -J /zap/wrk/out/zap_api_report.json \
  -r /zap/wrk/out/zap_api_report.html \
  -z "-config api.addrs.addr.name=.* -config api.addrs.addr.regex=true"
