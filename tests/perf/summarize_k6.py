#!/usr/bin/env python3
"""Ekstraktor ringkasan per-endpoint p50/p95/p99 & rasio baca/tulis dari output k6."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

ENDPOINTS: list[tuple[str, str, str]] = [
    ("ep_public_config", "BACA", "GET /api/v1/public/config"),
    ("ep_katalog_produk", "BACA", "GET /api/v1/katalog/produk"),
    ("ep_iuran_tagihan", "BACA", "GET /api/v1/iuran/tagihan"),
    ("ep_pesanan_list", "BACA", "GET /api/v1/pesanan"),
    ("ep_pembayaran_list", "BACA", "GET /api/v1/pembayaran"),
    ("ep_laporan", "BACA", "GET /api/v1/laporan"),
    ("ep_checkout", "TULIS", "POST /api/v1/toko/checkout"),
    ("ep_pembayaran_create", "TULIS", "POST /api/v1/pembayaran (JSON)"),
    ("ep_bukti_upload", "TULIS", "POST /api/v1/pembayaran (Multipart)"),
    ("ep_pembayaran_verify", "TULIS", "POST /api/v1/pembayaran/{id}/verifikasi"),
]


def mvals(metrics: dict[str, Any], name: str) -> dict[str, Any]:
    raw = metrics.get(name, {})
    if not isinstance(raw, dict):
        return {}
    vals = raw.get("values")
    return vals if isinstance(vals, dict) else raw


def main() -> int:
    ap = argparse.ArgumentParser(description="Summarize k6 load JSON per endpoint")
    ap.add_argument("--summary-json", required=True)
    ap.add_argument("--out-md", required=True)
    ap.add_argument("--out-txt", default="")
    args = ap.parse_args()

    data = json.loads(Path(args.summary_json).read_text(encoding="utf-8"))
    metrics = data.get("metrics", {})

    total_reqs = int(mvals(metrics, "http_reqs").get("count", 0))
    if total_reqs <= 0:
        raise AssertionError(f"k6 http_reqs.count harus > 0, dapat {total_reqs}")

    read_cnt = int(mvals(metrics, "kopdes_read_reqs").get("count", 0))
    write_cnt = int(mvals(metrics, "kopdes_write_reqs").get("count", 0))
    app_reqs = read_cnt + write_cnt
    write_pct = (write_cnt / app_reqs * 100.0) if app_reqs > 0 else 0.0

    dur = mvals(metrics, "http_req_duration")
    r_dur = mvals(metrics, "kopdes_read_duration")
    w_dur = mvals(metrics, "kopdes_write_duration")
    unexp = mvals(metrics, "kopdes_unexpected_error")
    http_fail = mvals(metrics, "http_req_failed")
    unexp_rate = float(unexp.get("rate", unexp.get("value", 0.0)))
    fail_rate = float(http_fail.get("rate", http_fail.get("value", 0.0)))

    md_lines = [
        "| Jenis | Endpoint | p50 (ms) | p95 (ms) | p99 (ms) |",
        "| :--- | :--- | ---: | ---: | ---: |",
    ]
    compact_parts: list[str] = []

    for key, kind, label in ENDPOINTS:
        mv = mvals(metrics, key)
        p50 = float(mv.get("med", 0.0))
        p95 = float(mv.get("p(95)", 0.0))
        p99 = float(mv.get("p(99)", 0.0))
        if p95 <= 0.0:
            raise AssertionError(f"Metrik endpoint {key} tidak memiliki p(95) > 0: {mv}")
        md_lines.append(f"| {kind} | `{label}` | {p50:.1f} | {p95:.1f} | {p99:.1f} |")
        short = key.removeprefix("ep_")
        compact_parts.append(f"{short}={p50:.1f}/{p95:.1f}/{p99:.1f}ms")

    summary_line = (
        f"total_reqs={total_reqs} read_reqs={read_cnt} write_reqs={write_cnt} write_pct={write_pct:.1f}% "
        f"overall=(p50={float(dur.get('med', 0)):.1f}ms p95={float(dur.get('p(95)', 0)):.1f}ms p99={float(dur.get('p(99)', 0)):.1f}ms) "
        f"read_slo=(p50={float(r_dur.get('med', 0)):.1f}ms p95={float(r_dur.get('p(95)', 0)):.1f}ms p99={float(r_dur.get('p(99)', 0)):.1f}ms) "
        f"write_slo=(p50={float(w_dur.get('med', 0)):.1f}ms p95={float(w_dur.get('p(95)', 0)):.1f}ms p99={float(w_dur.get('p(99)', 0)):.1f}ms) "
        f"http_err={fail_rate*100:.2f}% unexp_err={unexp_rate*100:.2f}%"
    )
    ep_line = " | ".join(compact_parts)

    out_md = Path(args.out_md)
    out_md.parent.mkdir(parents=True, exist_ok=True)
    out_md.write_text(summary_line + "\n\n" + "\n".join(md_lines) + "\n", encoding="utf-8")
    if args.out_txt:
        Path(args.out_txt).write_text(summary_line + "\n" + ep_line + "\n", encoding="utf-8")

    print(summary_line)
    print("\n".join(md_lines))
    print(f"::notice title=k6 Mix & SLO::{summary_line.replace('%', '%25')}")
    print(f"::notice title=k6 Per-Endpoint (p50/p95/p99)::{ep_line.replace('%', '%25')}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
