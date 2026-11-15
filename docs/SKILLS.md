# Daftar Skill Agen Kopdes Merah Putih

Repositori ini menggunakan **tiga skill lokal berfokus** di `.agents/skills/` sebagai aturan otoritatif pengembangan backend ramping:

| Nama Skill | Lokasi | Cakupan | Status |
| :--- | :--- | :--- | :--- |
| `lean-go-backend` | `.agents/skills/lean-go-backend/SKILL.md` | Batas 400 baris/handler, 60 baris/fungsi, domain per paket, wajib `sqlc`, error `%w`, anti-bloat | Aktif (Utama) |
| `kopdes-domain-rules` | `.agents/skills/kopdes-domain-rules/SKILL.md` | Lingkup satu koperasi, 4 peran, mesin status, Rupiah integer, enkripsi PII/KYC, fitur terlarang | Aktif (Utama) |
| `security-baseline-kopdes` | `.agents/skills/security-baseline-kopdes/SKILL.md` | Daftar periksa OWASP 12 butir per endpoint HTTP, sesi opaque, upload aman, rate limit DB | Aktif (Utama) |

## Catatan Skill Pihak Ketiga (Bagian 2.3 Prompt REVAMP)
- Kandidat skill pihak ketiga (`golang` MadAppGang/claude-code, `secure-webapp` hov172/secure-webapp-skill, `simplify-and-harden` pskoett/pskoett-ai-skills, `agent-skills` addyosmani) **tidak diunduh/dipakai sebagai salinan terpisah** pada fase revamp ini karena seluruh prinsip Go idiomatik, OWASP static checklist, dan batasan anti-bloat sudah dikonsolidasikan langsung ke tiga skill otoritatif di atas (sesuai aturan prompt: maksimum 4 skill, tanpa hook/skrip eksternal, dan aturan 3 skill lokal menang).
- Skill lama `.agents/skills/kopdes-db-validator/SKILL.md` (aturan 65 tabel multi-tenant RLS) telah dihapus.
