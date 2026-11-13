#!/usr/bin/env bash
# Memeriksa konflik port internal & ketersediaan port host sebelum menjalankan stack.
set -euo pipefail

ENV_FILE="${1:-.env.test}"
go run ./cmd/envcheck --ports --strict --env-file "${ENV_FILE}"
