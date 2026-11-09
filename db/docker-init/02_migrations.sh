#!/bin/bash
set -e

echo "=== Menjalankan Seluruh Migrasi Skema Basis Data Secara Berurutan ==="
for f in $(ls /docker-entrypoint-initdb.d/migrations/*_*.up.sql | sort); do
    echo "Mengeksekusi migrasi skema: $f"
    psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" -f "$f"
done
echo "=== Seluruh Migrasi Berhasil Diterapkan ==="
