#!/usr/bin/env python3
"""Pemindai OWASP ZAP per peran + audit jangkauan 36 endpoint OpenAPI."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
import urllib.parse
from pathlib import Path
from typing import Any

import yaml

from auth_helper import ROLE_ACCOUNTS, http_json, login_role, parse_env_file

DUMMY_UUID = "00000000-0000-0000-0000-000000000001"
ZAP_IMAGE = "ghcr.io/zaproxy/zaproxy:stable"


def extract_openapi_operations(spec: dict[str, Any]) -> list[tuple[str, str]]:
    ops: list[tuple[str, str]] = []
    for path, item in spec.get("paths", {}).items():
        if not isinstance(item, dict):
            continue
        for method in ("get", "post", "put", "patch", "delete"):
            if method in item:
                ops.append((method.upper(), path))
    return ops


def build_probe_target(method: str, path: str, session: dict[str, Any]) -> tuple[str, dict[str, Any] | None]:
    resolved = path
    if path == "/api/v1/anggota/{id}" and session.get("anggota_id"):
        resolved = resolved.replace("{id}", str(session["anggota_id"]))
    else:
        resolved = resolved.replace("{id}", DUMMY_UUID)
    resolved = resolved.replace("{tipe}", "ktp")
    if path == "/api/v1/laporan":
        resolved = "/api/v1/laporan?jenis=anggota"

    if method == "GET":
        return resolved, None
    if path == "/api/v1/pesanan/{id}/status":
        return resolved, {"status": "batal", "catatan": "reachability-probe"}
    if path == "/api/v1/auth/password":
        return resolved, {"new_password": "short"}
    return resolved, {}


def check_reachability(
    api_base: str,
    ops: list[tuple[str, str]],
    session: dict[str, Any],
    client_ip: str = "10.20.30.42",
) -> list[dict[str, Any]]:
    base = api_base.rstrip("/")
    hdrs = {
        "Cookie": f"{session['cookie_name']}={session['cookie_value']}",
        "X-Requested-With": "zap-reachability",
        "X-Forwarded-For": client_ip,
    }
    results: list[dict[str, Any]] = []
    for method, path in ops:
        if path in ("/api/v1/auth/logout", "/api/v1/auth/mfa/verify"):
            continue
        resolved, payload = build_probe_target(method, path, session)
        status, _, _ = http_json(method, f"{base}{resolved}", payload=payload, headers=hdrs)
        results.append({
            "method": method,
            "path": path,
            "status": status,
            "category": _classify_status(status),
        })
    return results


def _classify_status(status: int) -> str:
    if status in (401, 403):
        return "denied_401_403"
    if status >= 500 or status <= 0:
        return "server_error"
    return "reachable"


def render_runtime_openapi(src_path: Path, dst_path: Path, api_base: str) -> None:
    spec = yaml.safe_load(src_path.read_text(encoding="utf-8"))
    spec["servers"] = [{"url": api_base.rstrip("/")}]
    paths = spec.get("paths", {})
    for safe_omit in ("/api/v1/auth/logout", "/api/v1/auth/mfa/verify"):
        paths.pop(safe_omit, None)
    dst_path.write_text(yaml.safe_dump(spec, sort_keys=False, allow_unicode=True), encoding="utf-8")


def run_docker_zap(cmd: list[str], log_path: Path) -> int:
    proc = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, errors="replace")
    log_path.write_text(proc.stdout, encoding="utf-8")
    return proc.returncode


def count_log_errors(pg_log: str, api_log: str) -> dict[str, int]:
    counts = {"pg_log_errors": 0, "pg_syntax_errors": 0, "api_500": 0, "api_sql_errors": 0}
    if pg_log and Path(pg_log).exists():
        for ln in Path(pg_log).read_text(encoding="utf-8", errors="replace").splitlines():
            lo = ln.lower()
            if "error:" in lo or "fatal:" in lo:
                counts["pg_log_errors"] += 1
            if "syntax error" in lo or "sqlstate" in lo:
                counts["pg_syntax_errors"] += 1
    if api_log and Path(api_log).exists():
        for ln in Path(api_log).read_text(encoding="utf-8", errors="replace").splitlines():
            lo = ln.lower()
            if " 500 " in lo or "internal_server_error" in lo:
                counts["api_500"] += 1
            if "sqlstate" in lo or "syntax error" in lo:
                counts["api_sql_errors"] += 1
    return counts


def clean_otherinfo(raw: str) -> str:
    s = re.sub(r"\[[^\]]*\]", "[REDACTED]", raw or "")
    s = re.sub(r"'[^']*'|\"[^\"]*\"", "[REDACTED]", s)
    return " ".join(s.split())[:120]


def analyze_zap_reports(
    report_files: list[Path],
    fp_cfg: dict[str, Any],
    log_counts: dict[str, int],
) -> dict[str, Any]:
    guard = fp_cfg.get("guard", {})
    exclusions = fp_cfg.get("exclusions", [])
    guard_ok = (
        (not guard.get("require_zero_pg_syntax_errors", True) or log_counts["pg_syntax_errors"] == 0)
        and (not guard.get("require_zero_api_sql_errors", True) or log_counts["api_sql_errors"] == 0)
    )

    lines: list[str] = []
    med_alerts: list[str] = []
    triaged_fps: list[str] = []
    unexcluded_high: list[str] = []
    triage_rows: list[str] = []
    low_alerts = 0
    info_alerts = 0

    for rf in report_files:
        data = json.loads(rf.read_text(encoding="utf-8"))
        alerts = [a for s in data.get("site", []) for a in s.get("alerts", [])]
        highs = [a for a in alerts if str(a.get("riskcode")) == "3"]
        meds = [a for a in alerts if str(a.get("riskcode")) == "2"]
        lines.append(f"{rf.name}: total_alerts={len(alerts)} high={len(highs)} med={len(meds)}")

        for a in alerts:
            rc = str(a.get("riskcode"))
            if rc == "1":
                low_alerts += 1
            elif rc == "0":
                info_alerts += 1
            pid = str(a.get("pluginid", ""))
            insts = a.get("instances", [])
            paths = sorted({urllib.parse.urlparse(i.get("uri", "")).path or "/" for i in insts})[:8]
            pstr = ",".join(paths)
            lines.append(f"  - [risk={rc}:{a.get('riskdesc')}] plugin={pid} {a.get('name')} (count={a.get('count')}) paths=[{pstr}]")

            if rc == "2":
                med_alerts.append(f"[risk=2] {pid}:{a.get('name')}(count={a.get('count')},paths={pstr})")
                for inst in insts:
                    m = inst.get("method", "?")
                    p = urllib.parse.urlparse(inst.get("uri", "")).path or "/"
                    prm = inst.get("param", "") or "-"
                    triage_rows.append(f"{rf.name} plugin={pid} risk=2 method={m} path={p} param={prm} other={clean_otherinfo(inst.get('otherinfo', ''))}")
            elif rc == "3":
                if not insts:
                    unexcluded_high.append(f"{rf.name} plugin={pid}:{a.get('name')} (no instances)")
                for inst in insts:
                    m = inst.get("method", "?")
                    p = urllib.parse.urlparse(inst.get("uri", "")).path or "/"
                    prm = inst.get("param", "") or "-"
                    raw_oi = inst.get("otherinfo", "") or ""
                    matched = _match_fp(exclusions, guard_ok, pid, m, p, raw_oi)
                    if matched:
                        triaged_fps.append(f"plugin={pid} {m} {p} param={prm} guard_ok={guard_ok}")
                        triage_rows.append(f"[TRIAGED-FP] {rf.name} plugin={pid} risk=3 method={m} path={p} param={prm} other={clean_otherinfo(raw_oi)}")
                    else:
                        unexcluded_high.append(f"{rf.name} plugin={pid} {m} {p} param={prm}")
                        triage_rows.append(f"[UNEXCLUDED-HIGH] {rf.name} plugin={pid} risk=3 method={m} path={p} param={prm} other={clean_otherinfo(raw_oi)}")

    return {
        "guard_ok": guard_ok,
        "lines": lines,
        "med_alerts": med_alerts,
        "triaged_fps": triaged_fps,
        "unexcluded_high": unexcluded_high,
        "triage_rows": triage_rows,
        "low_alerts": low_alerts,
        "info_alerts": info_alerts,
    }


def _match_fp(exclusions: list[dict[str, Any]], guard_ok: bool, pid: str, method: str, path: str, raw_oi: str) -> dict[str, Any] | None:
    if not guard_ok:
        return None
    for ex in exclusions:
        if (
            str(ex.get("pluginid", "")) == str(pid)
            and str(ex.get("method", "")).upper() == method.upper()
            and str(ex.get("path", "")) == path
            and str(ex.get("otherinfo_contains", "")) in raw_oi
        ):
            return ex
    return None


def main() -> int:
    ap = argparse.ArgumentParser(description="Per-role OWASP ZAP OpenAPI scan & reachability audit")
    ap.add_argument("--profile", required=True)
    ap.add_argument("--role", required=True)
    ap.add_argument("--api-base", required=True)
    ap.add_argument("--env-file", required=True)
    ap.add_argument("--password", required=True)
    ap.add_argument("--out-dir", required=True)
    ap.add_argument("--openapi", default="docs/openapi.yaml")
    ap.add_argument("--fp-config", default="tests/security/zap_false_positives.json")
    ap.add_argument("--zap-mode", choices=("paced", "full"), default="paced")
    ap.add_argument("--run-baseline", choices=("true", "false"), default="false")
    ap.add_argument("--rate-limit-rps", default="30")
    ap.add_argument("--pg-log", default="")
    ap.add_argument("--api-log", default="")
    args = ap.parse_args()

    out_dir = Path(args.out_dir).resolve()
    out_dir.mkdir(parents=True, exist_ok=True)
    env_map = parse_env_file(args.env_file)
    email = ROLE_ACCOUNTS[args.role]
    session = login_role(args.api_base, email=email, password=args.password, aes_master_key=env_map.get("AES_MASTER_KEY", ""), client_ip="10.20.30.15")

    spec = yaml.safe_load(Path(args.openapi).read_text(encoding="utf-8"))
    ops = extract_openapi_operations(spec)
    reach = check_reachability(args.api_base, ops, session)

    runtime_spec_name = f"openapi_runtime_{args.profile}.yaml"
    render_runtime_openapi(Path(args.openapi), out_dir / runtime_spec_name, args.api_base)
    subprocess.run(["chmod", "-R", "a+rwx", str(out_dir)], check=False)

    report_files: list[Path] = []
    if args.run_baseline == "true":
        base_json = f"zap_baseline_{args.profile}.json"
        base_html = f"zap_baseline_{args.profile}.html"
        rc_b = run_docker_zap(
            [
                "docker", "run", "--rm", "--network", "host", "-v", f"{out_dir}:/zap/wrk:rw", ZAP_IMAGE,
                "zap-baseline.py", "-t", f"{args.api_base.rstrip('/')}/api/v1/public/config", "-I",
                "-J", base_json, "-r", base_html,
            ],
            out_dir / f"zap_baseline_{args.profile}.log",
        )
        print((out_dir / f"zap_baseline_{args.profile}.log").read_text(encoding="utf-8", errors="replace"))
        if rc_b != 0:
            print(f"::error title=ZAP Baseline Failed ({args.profile})::exit={rc_b}")
            return rc_b
        report_files.append(out_dir / base_json)

    cookie_hdr = f"{session['cookie_name']}={session['cookie_value']}"
    z_opts = [
        "-config replacer.full_list(0).description=auth",
        "-config replacer.full_list(0).enabled=true",
        "-config replacer.full_list(0).matchtype=REQ_HEADER",
        "-config replacer.full_list(0).matchstr=Cookie",
        "-config replacer.full_list(0).regex=false",
        f"-config replacer.full_list(0).replacement={cookie_hdr}",
        "-config replacer.full_list(1).description=csrf",
        "-config replacer.full_list(1).enabled=true",
        "-config replacer.full_list(1).matchtype=REQ_HEADER",
        "-config replacer.full_list(1).matchstr=X-Requested-With",
        "-config replacer.full_list(1).regex=false",
        "-config replacer.full_list(1).replacement=zap-scan",
        "-config replacer.full_list(2).description=ip",
        "-config replacer.full_list(2).enabled=true",
        "-config replacer.full_list(2).matchtype=REQ_HEADER",
        "-config replacer.full_list(2).matchstr=X-Forwarded-For",
        "-config replacer.full_list(2).regex=false",
        "-config replacer.full_list(2).replacement=10.20.30.41",
    ]
    if args.zap_mode == "paced":
        z_opts.extend(["-config scanner.threadPerHost=1", "-config scanner.delayInMs=50"])
    else:
        z_opts.extend(["-config scanner.threadPerHost=4", "-config scanner.delayInMs=0"])

    api_json = f"zap_api_{args.profile}.json"
    api_html = f"zap_api_{args.profile}.html"
    rc_a = run_docker_zap(
        [
            "docker", "run", "--rm", "--network", "host", "-v", f"{out_dir}:/zap/wrk:rw", ZAP_IMAGE,
            "zap-api-scan.py", "-t", f"/zap/wrk/{runtime_spec_name}", "-f", "openapi", "-I",
            "-J", api_json, "-r", api_html, "-z", " ".join(z_opts),
        ],
        out_dir / f"zap_api_{args.profile}.log",
    )
    print((out_dir / f"zap_api_{args.profile}.log").read_text(encoding="utf-8", errors="replace"))
    if rc_a != 0:
        print(f"::error title=ZAP API Scan Failed ({args.profile})::exit={rc_a}")
        return rc_a
    report_files.append(out_dir / api_json)

    # Verify active session survived the entire scan, then audit mfa/verify & logout reachability
    ses_hdr = {"Cookie": cookie_hdr, "X-Requested-With": "zap-post-check", "X-Forwarded-For": "10.20.30.42"}
    me_st, me_body, _ = http_json("GET", f"{args.api_base.rstrip('/')}/api/v1/auth/me", headers=ses_hdr)
    post_scan_ok = me_st == 200 and me_body.get("data", {}).get("scope") == "full"

    if session.get("mfa_verify_status") is not None:
        mfa_st = int(session["mfa_verify_status"])
    else:
        mfa_st, _, _ = http_json("POST", f"{args.api_base.rstrip('/')}/api/v1/auth/mfa/verify", payload={"code": "000000"}, headers=ses_hdr)
    reach.append({"method": "POST", "path": "/api/v1/auth/mfa/verify", "status": mfa_st, "category": _classify_status(mfa_st)})

    lo_st, _, _ = http_json("POST", f"{args.api_base.rstrip('/')}/api/v1/auth/logout", payload={}, headers=ses_hdr)
    reach.append({"method": "POST", "path": "/api/v1/auth/logout", "status": lo_st, "category": _classify_status(lo_st)})

    reachable_ops = [r for r in reach if r["category"] == "reachable"]
    denied_ops = [r for r in reach if r["category"] == "denied_401_403"]
    err_ops = [r for r in reach if r["category"] == "server_error"]

    reach_summary = {
        "profile": args.profile,
        "role": args.role,
        "mfa_enabled": session["mfa_enabled"],
        "mfa_verified": session["mfa_verified"],
        "mfa_status": session["mfa_status"],
        "rate_limit_rps": args.rate_limit_rps,
        "zap_mode": args.zap_mode,
        "post_scan_session_ok": post_scan_ok,
        "total_operations": len(reach),
        "reachable_count": len(reachable_ops),
        "denied_401_403_count": len(denied_ops),
        "server_error_count": len(err_ops),
        "reachable_routes": [f"{r['method']} {r['path']} ({r['status']})" for r in reachable_ops],
        "denied_routes": [f"{r['method']} {r['path']} ({r['status']})" for r in denied_ops],
        "error_routes": [f"{r['method']} {r['path']} ({r['status']})" for r in err_ops],
    }
    (out_dir / f"reachability_{args.profile}.json").write_text(json.dumps(reach_summary, indent=2) + "\n", encoding="utf-8")

    pg_cid_proc = subprocess.run(["docker", "ps", "-q", "--filter", "ancestor=postgres:16-alpine"], capture_output=True, text=True)
    pg_log_path = args.pg_log
    if pg_cid_proc.stdout.strip():
        cid = pg_cid_proc.stdout.strip().splitlines()[0]
        p_out = subprocess.run(["docker", "logs", cid], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, errors="replace")
        pg_dump = out_dir / f"pg_dast_{args.profile}.log"
        pg_dump.write_text(p_out.stdout, encoding="utf-8")
        pg_log_path = str(pg_dump)

    log_counts = count_log_errors(pg_log_path, args.api_log)
    fp_cfg = json.loads(Path(args.fp_config).read_text(encoding="utf-8"))
    analysis = analyze_zap_reports(report_files, fp_cfg, log_counts)

    notice_summary = (
        f"role={args.role} mfa={session['mfa_status']} rps={args.rate_limit_rps} mode={args.zap_mode} "
        f"reachable={len(reachable_ops)}/{len(reach)} denied={len(denied_ops)} err500={len(err_ops)} "
        f"post_session_ok={post_scan_ok} | " + " | ".join(analysis["lines"])
    ).replace("%", "%25")
    med_items = analysis["med_alerts"] + [f"[HIGH] {h}" for h in analysis["unexcluded_high"]]
    med_msg = (
        (" | ".join(med_items) if med_items else "NONE (0 Unexcluded High, 0 Medium)")
        + f" | triaged_fp={len(analysis['triaged_fps'])} low_alerts={analysis['low_alerts']} info_alerts={analysis['info_alerts']}"
    ).replace("%", "%25")
    triage_msg = (
        f"pg_err={log_counts['pg_log_errors']} pg_syntax={log_counts['pg_syntax_errors']} "
        f"api_500={log_counts['api_500']} api_sql={log_counts['api_sql_errors']} guard_ok={analysis['guard_ok']} | "
        + (" || ".join(analysis["triage_rows"][:10]) if analysis["triage_rows"] else "no-med-high-instances")
    ).replace("%", "%25")

    (out_dir / f"zap_summary_{args.profile}.txt").write_text(notice_summary + "\n" + med_msg + "\n" + triage_msg + "\n", encoding="utf-8")
    print(f"::notice title=ZAP Role Summary ({args.profile})::{notice_summary}")
    print(f"::notice title=ZAP Medium+ ({args.profile})::{med_msg}")
    print(f"::notice title=ZAP Triage Evidence ({args.profile})::{triage_msg}")

    if not post_scan_ok:
        print(f"::error title=ZAP Session Revoked Mid-Scan ({args.profile})::GET /api/v1/auth/me status={me_st}")
        return 1
    if len(err_ops) > 0:
        print(f"::error title=ZAP Reachability 5xx ({args.profile})::{err_ops}")
        return 1
    if analysis["unexcluded_high"]:
        print(f"::error title=ZAP Unexcluded HIGH ({args.profile})::{' | '.join(analysis['unexcluded_high'])}")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
