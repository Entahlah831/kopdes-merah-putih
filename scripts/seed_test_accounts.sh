#!/usr/bin/env sh
set -eu

ENV_FILE_ARG="${1:-${ENV_FILE:-.env.test}}"
BASE_NAME="$(basename "$ENV_FILE_ARG")"

case "$BASE_NAME" in
  .env|.env.local|.env.production|.env.live)
    echo "DILARANG: seed_test_accounts tidak boleh dijalankan terhadap berkas $BASE_NAME" >&2
    exit 1
    ;;
esac

shift || true
exec go run ./cmd/seed_test_accounts --env-file "$ENV_FILE_ARG" "$@"
