#!/usr/bin/env bash
# Memeriksa konflik port internal & ketersediaan port host sebelum menjalankan stack.
set -euo pipefail

ENV_FILE="${1:-.env.test}"
ABS_ENV="$(cd "$(dirname "${ENV_FILE}")" && pwd)/$(basename "${ENV_FILE}")"
go -C backend run ./cmd/envcheck --ports --strict --env-file "${ABS_ENV}"
