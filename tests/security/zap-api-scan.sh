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
OUT_DIR="${OUT_DIR:-reports/final}"
mkdir -p "${OUT_DIR}"

echo "[ZAP 1/2] Menjalankan OWASP ZAP Baseline Scan terhadap ${TARGET_URL}/api/v1/public/config"
docker run --rm --network host \
  -v "$(pwd)/${OUT_DIR}:/zap/wrk:rw" \
  ghcr.io/zaproxy/zaproxy:stable \
  zap-baseline.py \
  -t "${TARGET_URL}/api/v1/public/config" \
  -I \
  -J zap_baseline_report.json \
  -r zap_baseline_report.html

TARGET_URL="${TARGET_URL}" OUT_DIR="${OUT_DIR}" python3 - <<'PY'
import os, re
raw = open("docs/openapi.yaml", "r", encoding="utf-8").read()
rendered = re.sub(
    r"^servers:\n.*?(?=^[a-zA-Z0-9_-]+:)",
    f"servers:\n- url: {os.environ['TARGET_URL']}\n",
    raw,
    count=1,
    flags=re.M | re.S,
)
with open(os.path.join(os.environ["OUT_DIR"], "openapi_runtime.yaml"), "w", encoding="utf-8") as f:
    f.write(rendered)
PY

echo "[ZAP 2/2] Menjalankan OWASP ZAP OpenAPI API Scan terhadap ${TARGET_URL}"
docker run --rm --network host \
  -v "$(pwd)/${OUT_DIR}:/zap/wrk:rw" \
  ghcr.io/zaproxy/zaproxy:stable \
  zap-api-scan.py \
  -t /zap/wrk/openapi_runtime.yaml \
  -f openapi \
  -I \
  -J zap_api_report.json \
  -r zap_api_report.html \
  -z "-config scanner.threadPerHost=1 -config scanner.delayInMs=50"
