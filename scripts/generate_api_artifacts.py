"""Generate Postman Collection v2.1 dan TypeScript Client dari docs/openapi.yaml."""

import json
import os
import yaml

OPENAPI_PATH = os.path.join("docs", "openapi.yaml")
POSTMAN_PATH = os.path.join("docs", "kopdes.postman_collection.json")
TS_CLIENT_PATH = os.path.join("docs", "kopdesClient.ts")


def to_ts_type(schema, schemas):
    if not schema:
        return "unknown"
    if "$ref" in schema:
        name = schema["$ref"].split("/")[-1]
        return name
    t = schema.get("type")
    if t in ("integer", "number"):
        return "number"
    if t == "boolean":
        return "boolean"
    if t == "string":
        if "enum" in schema:
            return " | ".join(f'"{v}"' for v in schema["enum"])
        return "string"
    if t == "array":
        return f"Array<{to_ts_type(schema.get('items', {}), schemas)}>"
    if t == "object":
        return "Record<string, unknown>"
    return "unknown"


def sample_for_schema(schema, schemas):
    if not schema:
        return {}
    if "$ref" in schema:
        schema = schemas.get(schema["$ref"].split("/")[-1], {})
    props = schema.get("properties", {})
    out = {}
    for k, prop in props.items():
        if "example" in prop:
            out[k] = prop["example"]
        elif prop.get("type") == "string":
            if prop.get("format") == "uuid":
                out[k] = "00000000-0000-0000-0000-000000000000"
            elif prop.get("format") == "email":
                out[k] = "user@example.invalid"
            elif prop.get("format") == "date":
                out[k] = "2026-11-13"
            elif "enum" in prop:
                out[k] = prop["enum"][0]
            else:
                out[k] = f"<{k}>"
        elif prop.get("type") in ("integer", "number"):
            out[k] = 1
        elif prop.get("type") == "boolean":
            out[k] = False
        elif prop.get("type") == "array":
            out[k] = []
    return out


def main():
    with open(OPENAPI_PATH, "r", encoding="utf-8") as f:
        spec = yaml.safe_load(f)

    schemas = spec.get("components", {}).get("schemas", {})
    paths = spec.get("paths", {})

    # 1. Postman Collection v2.1 (variabel baseUrl & accessToken — bebas hardcode port/rahasia)
    folders = {}
    for path, methods in paths.items():
        for method, op in methods.items():
            tag = (op.get("tags") or ["Umum"])[0]
            folders.setdefault(tag, [])
            raw_url = "{{baseUrl}}" + path.replace("{", ":").replace("}", "")
            url_obj = {
                "raw": raw_url,
                "host": ["{{baseUrl}}"],
                "path": [seg for seg in path.strip("/").split("/") if seg],
            }
            headers = [{"key": "Content-Type", "value": "application/json"}]
            sec = op.get("security") or []
            if any("MetricsBearerAuth" in s for s in sec):
                headers.append({"key": "Authorization", "value": "Bearer {{metricsToken}}"})
            elif any("BearerAuth" in s for s in sec):
                headers.append({"key": "Authorization", "value": "Bearer {{accessToken}}"})
            for p in op.get("parameters", []):
                if p.get("in") == "header":
                    headers.append({"key": p["name"], "value": f"{{{{{p['name']}}}}}"})

            req_obj = {
                "method": method.upper(),
                "header": headers,
                "url": url_obj,
                "description": op.get("summary", ""),
            }
            rb = op.get("requestBody", {}).get("content", {})
            if "application/json" in rb:
                s = rb["application/json"].get("schema", {})
                req_obj["body"] = {
                    "mode": "raw",
                    "raw": json.dumps(sample_for_schema(s, schemas), indent=2),
                    "options": {"raw": {"language": "json"}},
                }

            folders[tag].append({"name": f"{method.upper()} {path}", "request": req_obj})

    collection = {
        "info": {
            "name": spec["info"]["title"],
            "description": spec["info"]["description"],
            "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
            "_kopdes_version": spec["info"]["version"],
        },
        "variable": [
            {"key": "baseUrl", "value": "http://127.0.0.1:${API_PORT}"},
            {"key": "accessToken", "value": ""},
            {"key": "refreshToken", "value": ""},
            {"key": "metricsToken", "value": ""},
            {"key": "Idempotency-Key", "value": ""},
        ],
        "item": [{"name": k, "item": v} for k, v in folders.items()],
    }
    with open(POSTMAN_PATH, "w", encoding="utf-8", newline="\n") as f:
        json.dump(collection, f, indent=2, ensure_ascii=False)
        f.write("\n")

    # 2. TypeScript Client
    lines = [
        "// GENERATED FROM docs/openapi.yaml via scripts/generate_api_artifacts.py — DO NOT EDIT MANUALLY.",
        "",
        "export interface APIError {",
        "  code: string;",
        "  message: string;",
        "  details?: unknown;",
        "}",
        "",
        "export interface APIResponse<T = unknown> {",
        "  success: boolean;",
        "  data?: T;",
        "  meta?: Record<string, unknown>;",
        "  error?: APIError;",
        "}",
        "",
    ]

    for sname, sdef in schemas.items():
        if sname in ("APIError", "APIResponseGeneric"):
            continue
        req_fields = set(sdef.get("required", []))
        props = sdef.get("properties", {})
        lines.append(f"export interface {sname} {{")
        for pname, pdef in props.items():
            opt = "" if pname in req_fields else "?"
            lines.append(f"  {pname}{opt}: {to_ts_type(pdef, schemas)};")
        lines.append("}")
        lines.append("")

    lines.extend([
        "export type KopdesFetcher = (path: string, init?: RequestInit) => Promise<Response>;",
        "",
        "export function createKopdesEndpoints(fetcher: KopdesFetcher) {",
        "  return {",
    ])

    for path, methods in paths.items():
        for method, op in methods.items():
            op_id = op["operationId"]
            path_params = []
            has_idemp = False
            for p in op.get("parameters", []):
                if p.get("in") == "path":
                    path_params.append(p["name"])
                elif p.get("in") == "header" and p.get("name") == "Idempotency-Key":
                    has_idemp = True

            rb = op.get("requestBody", {}).get("content", {})
            body_type = None
            is_multipart = False
            if "application/json" in rb:
                s = rb["application/json"].get("schema", {})
                if "$ref" in s:
                    body_type = s["$ref"].split("/")[-1]
                else:
                    body_type = "Record<string, unknown>"
            elif "multipart/form-data" in rb:
                body_type = "FormData"
                is_multipart = True

            args = [f"{pp}: string" for pp in path_params]
            if body_type:
                args.append(f"body: {body_type}")
            if has_idemp:
                args.append("idempotencyKey?: string")

            ts_path = path
            for pp in path_params:
                ts_path = ts_path.replace(f"{{{pp}}}", f"${{encodeURIComponent({pp})}}")
            url_expr = f"`{ts_path}`"

            opts = [f'method: "{method.upper()}"']
            if has_idemp:
                opts.append('headers: idempotencyKey ? { "Idempotency-Key": idempotencyKey } : undefined')
            if body_type:
                opts.append("body: body" if is_multipart else "body: JSON.stringify(body)")

            lines.append(f"    {op_id}: ({', '.join(args)}) =>")
            lines.append(f"      fetcher({url_expr}, {{ {', '.join(opts)} }}),")

    lines.extend(["  };", "}"])
    with open(TS_CLIENT_PATH, "w", encoding="utf-8", newline="\n") as f:
        f.write("\n".join(lines) + "\n")

    sync_frontend_tester_and_roles_table(spec)
    print(f"[OK] Generated {POSTMAN_PATH}, {TS_CLIENT_PATH}, frontend-tester/openapi.json, frontend-tester/roles_matrix.json from {OPENAPI_PATH}")


def _format_role_cell(val):
    if val == 200 or val == "allow":
        return "Ya"
    if isinstance(val, list):
        return "Ya"
    if val == 403:
        return "Tidak (403)"
    if val == 401:
        return "401"
    return str(val)


def sync_frontend_tester_and_roles_table(spec):
    import shutil

    ui_dir = os.path.abspath("frontend-tester")
    os.makedirs(ui_dir, exist_ok=True)

    # 1. Emit frontend-tester/openapi.json directly from docs/openapi.yaml
    openapi_json_path = os.path.join(ui_dir, "openapi.json")
    with open(openapi_json_path, "w", encoding="utf-8", newline="\n") as f:
        json.dump(spec, f, indent=2, ensure_ascii=False)
        f.write("\n")

    # 2. Sync docs/roles_matrix.json -> frontend-tester/roles_matrix.json if present
    docs_matrix_path = os.path.join("docs", "roles_matrix.json")
    if not os.path.exists(docs_matrix_path):
        return
    ui_matrix_path = os.path.join(ui_dir, "roles_matrix.json")
    shutil.copyfile(docs_matrix_path, ui_matrix_path)

    with open(docs_matrix_path, "r", encoding="utf-8") as f:
        matrix = json.load(f)

    # 3. Render Markdown table inside docs/FRONTEND_GUIDE.md between BEGIN_ROLES_MATRIX / END_ROLES_MATRIX
    fg_path = os.path.join("docs", "FRONTEND_GUIDE.md")
    if not os.path.exists(fg_path):
        return
    with open(fg_path, "r", encoding="utf-8") as f:
        fg_text = f.read()

    begin_marker = "<!-- BEGIN_ROLES_MATRIX -->"
    end_marker = "<!-- END_ROLES_MATRIX -->"
    if begin_marker not in fg_text or end_marker not in fg_text:
        return

    roles = matrix.get("roles", ["admin", "pengurus", "bendahara", "ketua", "pengawas", "operator", "anggota"])
    header_cols = ["Method", "Path"] + roles + ["Unauth", "Cross-Tenant", "Catatan"]
    md_lines = [
        begin_marker,
        "",
        "| " + " | ".join(header_cols) + " |",
        "|" + "|".join(["---"] * len(header_cols)) + "|",
    ]
    for ep in matrix.get("endpoints", []):
        r_map = ep.get("roles", {})
        row = [
            f"`{ep['method'].upper()}`",
            f"`{ep['path']}`",
        ]
        for r in roles:
            row.append(_format_role_cell(r_map.get(r)))
        row.append(str(ep.get("unauthenticated", "")))
        row.append(str(ep.get("cross_tenant", "")).replace("|", "/"))
        row.append(str(ep.get("notes", "")).replace("|", "/"))
        md_lines.append("| " + " | ".join(row) + " |")
    md_lines.extend(["", end_marker])

    start_idx = fg_text.index(begin_marker)
    end_idx = fg_text.index(end_marker) + len(end_marker)
    updated_fg = fg_text[:start_idx] + "\n".join(md_lines) + fg_text[end_idx:]
    with open(fg_path, "w", encoding="utf-8", newline="\n") as f:
        f.write(updated_fg)


if __name__ == "__main__":
    main()
