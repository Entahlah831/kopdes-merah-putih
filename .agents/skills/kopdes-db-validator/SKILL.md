---
name: kopdes-db-validator
description: Validasi skema PostgreSQL 16 Kopdes, jalankan migrasi, dan generate kode Go sqlc.
---
# Instruksi Skill Database Kopdes
1. Jalankan PostgreSQL 16 lokal dengan docker compose up -d postgres.
2. Jalankan sqlc generate setiap kali ada perubahan pada db/queries/kopdes.sql atau db/migrations/.
3. Pastikan go test ./... lulus untuk pengujian konkurensi kode unik QRIS (1..999) dan RLS lintas koperasi.
