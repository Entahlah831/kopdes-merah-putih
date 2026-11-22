#!/usr/bin/env python3
"""Helper autentikasi & MFA TOTP deterministik untuk akun seeder tes Kopdes."""

from __future__ import annotations

import argparse
import hashlib
import hmac
import http.cookies
import json
import struct
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any

ROLE_ACCOUNTS: dict[str, str] = {
    "admin": "admin@kopdes-test.local",
    "bendahara": "bendahara@kopdes-test.local",
    "petugas_toko": "petugas@kopdes-test.local",
    "anggota": "anggota1@kopdes-test.local",
    "anggota2": "anggota2@kopdes-test.local",
}


def parse_env_file(path: str | Path) -> dict[str, str]:
    env_map: dict[str, str] = {}
    for raw in Path(path).read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, val = line.split("=", 1)
        key = key.strip()
        val = val.strip()
        if len(val) >= 2 and ((val[0] == '"' and val[-1] == '"') or (val[0] == "'" and val[-1] == "'")):
            val = val[1:-1]
        env_map[key] = val
    return env_map


def deterministic_totp_secret_bytes(aes_master_key: str, email: str) -> bytes:
    """Sesuai 1:1 dengan testseed.DeterministicTOTPSecret di internal/testseed/seed.go."""
    digest = hmac.new(
        aes_master_key.encode("utf-8"),
        email.strip().lower().encode("utf-8"),
        hashlib.sha256,
    ).digest()
    return digest[:20]


def generate_totp_code(secret_bytes: bytes, timestamp: float | None = None, offset_steps: int = 0) -> str:
    """Menghasilkan kode TOTP 6 digit RFC 6238 (SHA-1, periode 30 detik)."""
    ts = int(time.time() if timestamp is None else timestamp)
    counter = (ts // 30) + offset_steps
    msg = struct.pack(">Q", counter)
    digest = hmac.new(secret_bytes, msg, hashlib.sha1).digest()
    offset = digest[-1] & 0x0F
    binary_code = struct.unpack(">I", digest[offset : offset + 4])[0] & 0x7FFFFFFF
    return f"{binary_code % 1_000_000:06d}"


def http_json(
    method: str,
    url: str,
    payload: dict[str, Any] | None = None,
    headers: dict[str, str] | None = None,
    timeout: int = 15,
) -> tuple[int, dict[str, Any], dict[str, str]]:
    req_headers = {"Accept": "application/json", "X-Requested-With": "kopdes-auth-helper"}
    if headers:
        req_headers.update(headers)
    data_bytes = None
    if payload is not None:
        data_bytes = json.dumps(payload).encode("utf-8")
        req_headers.setdefault("Content-Type", "application/json")
    req = urllib.request.Request(url, data=data_bytes, headers=req_headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read().decode("utf-8", errors="replace")
            cookies = _extract_cookies(resp.headers.get_all("Set-Cookie") or [])
            return resp.status, _safe_json(raw), cookies
    except urllib.error.HTTPError as exc:
        raw = exc.read().decode("utf-8", errors="replace")
        cookies = _extract_cookies(exc.headers.get_all("Set-Cookie") or [])
        return exc.code, _safe_json(raw), cookies


def _safe_json(raw: str) -> dict[str, Any]:
    if not raw.strip():
        return {}
    try:
        parsed = json.loads(raw)
        return parsed if isinstance(parsed, dict) else {"raw": parsed}
    except json.JSONDecodeError:
        return {"raw_text": raw}


def _extract_cookies(set_cookie_headers: list[str]) -> dict[str, str]:
    jar: dict[str, str] = {}
    for hdr in set_cookie_headers:
        c = http.cookies.SimpleCookie()
        c.load(hdr)
        for name, morsel in c.items():
            if morsel.value:
                jar[name] = morsel.value
    return jar


def login_role(
    api_base: str,
    email: str,
    password: str,
    aes_master_key: str,
    client_ip: str = "10.20.30.10",
) -> dict[str, Any]:
    base = api_base.rstrip("/")
    status, body, cookies = http_json(
        "POST",
        f"{base}/api/v1/auth/login",
        payload={"identifier": email, "password": password},
        headers={"X-Forwarded-For": client_ip},
    )
    if status != 200:
        raise RuntimeError(f"Login gagal untuk {email}: HTTP {status} body={body}")

    data = body.get("data", {}) if isinstance(body.get("data"), dict) else {}
    cookie_name = ""
    cookie_val = ""
    for candidate in ("__Host-kopdes_session", "kopdes_session"):
        if candidate in cookies:
            cookie_name = candidate
            cookie_val = cookies[candidate]
            break
    bearer_tok = str(data.get("session_token") or "")
    if not cookie_val and bearer_tok:
        cookie_name = "kopdes_session"
        cookie_val = bearer_tok
    if not cookie_val:
        raise RuntimeError(f"Sesi cookie maupun token tidak ditemukan untuk {email}")

    session_headers = {
        "Cookie": f"{cookie_name}={cookie_val}",
        "X-Forwarded-For": client_ip,
    }

    mfa_required = bool(data.get("mfa_required", False))
    mfa_verified = False
    mfa_status: int | None = None
    if mfa_required:
        if not aes_master_key:
            raise RuntimeError(f"AES_MASTER_KEY wajib disetel untuk TOTP MFA akun {email}")
        secret_bytes = deterministic_totp_secret_bytes(aes_master_key, email)
        verified_ok = False
        last_err: dict[str, Any] = {}
        for step_off in (0, 1):
            totp_code = generate_totp_code(secret_bytes, offset_steps=step_off)
            m_status, m_body, _ = http_json(
                "POST",
                f"{base}/api/v1/auth/mfa/verify",
                payload={"code": totp_code},
                headers=session_headers,
            )
            mfa_status = m_status
            last_err = m_body
            if m_status == 200 and m_body.get("data", {}).get("scope") == "full":
                verified_ok = True
                mfa_verified = True
                break
        if not verified_ok:
            raise RuntimeError(f"Verifikasi MFA TOTP gagal untuk {email}: status={mfa_status} body={last_err}")

    me_status, me_body, _ = http_json("GET", f"{base}/api/v1/auth/me", headers=session_headers)
    me_data = me_body.get("data", {}) if isinstance(me_body.get("data"), dict) else {}
    if me_status != 200 or me_data.get("scope") != "full":
        raise RuntimeError(f"Validasi /api/v1/auth/me gagal untuk {email}: status={me_status} body={me_body}")

    mfa_state = "verified" if mfa_verified else "not_required"
    return {
        "email": email,
        "role": me_data.get("peran") or data.get("peran") or "",
        "user_id": me_data.get("user_id") or data.get("user_id") or "",
        "anggota_id": me_data.get("anggota_id") or "",
        "scope": me_data.get("scope"),
        "mfa_enabled": bool(me_data.get("mfa_enabled", False)),
        "mfa_verified": mfa_verified,
        "mfa_status": mfa_state,
        "mfa_verify_status": mfa_status,
        "cookie_name": cookie_name,
        "cookie_value": cookie_val,
        "bearer_token": bearer_tok or cookie_val,
    }


def main() -> int:
    ap = argparse.ArgumentParser(description="Authenticate Kopdes test accounts (including MFA TOTP)")
    ap.add_argument("--api-base", required=True)
    ap.add_argument("--env-file", required=True)
    ap.add_argument("--password", required=True)
    ap.add_argument("--roles", default="admin,bendahara,petugas_toko,anggota")
    ap.add_argument("--out-json", default="")
    args = ap.parse_args()

    env_map = parse_env_file(args.env_file)
    aes_key = env_map.get("AES_MASTER_KEY", "")
    sessions: dict[str, Any] = {}
    for idx, role_key in enumerate(r.strip() for r in args.roles.split(",") if r.strip()):
        email = ROLE_ACCOUNTS.get(role_key, role_key)
        sessions[role_key] = login_role(
            args.api_base,
            email=email,
            password=args.password,
            aes_master_key=aes_key,
            client_ip=f"10.20.30.{20 + idx}",
        )

    out_text = json.dumps(sessions, indent=2)
    if args.out_json:
        Path(args.out_json).parent.mkdir(parents=True, exist_ok=True)
        Path(args.out_json).write_text(out_text + "\n", encoding="utf-8")
    else:
        print(out_text)
    return 0


if __name__ == "__main__":
    sys.exit(main())
