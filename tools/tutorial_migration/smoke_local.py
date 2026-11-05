#!/usr/bin/env python3
"""Exercise tutorial authorization, shared media, publishing and revisions locally.

Creates only temporary tutorial records and removes them in finally. Credentials
come from the existing local acceptance stack login file; tokens stay in memory.
"""
import argparse
import base64
import json
import re
import sys
import urllib.error
import urllib.parse
import uuid
from pathlib import Path

from migrate import Client, read_json, write_json


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--login-file", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    login = read_json(args.login_file)
    origin = login["url"].rstrip("/")
    if urllib.parse.urlsplit(origin).hostname not in ("localhost", "127.0.0.1", "::1"):
        raise ValueError("acceptance smoke is local-only")
    anonymous = Client(origin, proxy="")
    result = anonymous.json("/api/v1/auth/login", "POST", {"email": login["email"], "password": login["password"]})
    admin = Client(origin, result["access_token"], "")
    ids, findings = [], []
    seed = "tutorial-smoke-" + uuid.uuid4().hex[:10]
    def check(name, condition):
        if not condition:
            raise AssertionError(name)
        findings.append(name)
    def denied(client, path, expected, method="GET", value=None):
        try:
            client.json(path, method, value)
        except RuntimeError as error:
            check(f"{method} {path.split('?')[0]} denied {expected}", str(error).startswith(f"HTTP {expected}:"))
        else:
            raise AssertionError("request unexpectedly allowed")
    def get_admin(identity):
        return admin.json(f"/api/v1/admin/tutorials/{identity}")["tutorial"]
    def save(document, content):
        return admin.json(f"/api/v1/admin/tutorials/{document['id']}", "PATCH", {"version": document["version"], "content_markdown": content})["tutorial"]
    try:
        denied(anonymous, "/api/v1/admin/tutorials", 401)
        for suffix in ("one", "two"):
            doc = admin.json("/api/v1/admin/tutorials", "POST", {"slug": seed + "-" + suffix, "title": "临时验收 " + suffix, "content_markdown": "# Original\n\nFirst published body", "is_public": False})
            ids.append(doc["id"])
        first, second = map(get_admin, ids)
        check("new tutorials are drafts", first["status"] == "draft" and second["status"] == "draft")
        denied(admin, f"/api/v1/tutorials/{first['slug']}", 404)
        # Valid 1x1 PNG, written only to an acceptance scratch directory.
        media_path = Path(args.output).parent / "smoke-pixel.png"
        media_path.parent.mkdir(parents=True, exist_ok=True)
        media_path.write_bytes(base64.b64decode("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+j5iUAAAAASUVORK5CYII="))
        asset = admin.upload(first["id"], media_path, "验收图片")
        duplicate = admin.upload(second["id"], media_path, "相同文件")
        check("duplicate upload reuses immutable blob", asset["public_url"] == duplicate["public_url"] and asset["id"] == duplicate["id"])
        data, _ = anonymous.request(asset["preview_url"])
        check("first image survives duplicate upload", data == media_path.read_bytes())
        denied(anonymous, asset["public_url"], 403)
        markdown = '# Original\n\nFirst published body\n\n![pixel](' + asset["public_url"] + ')\n\n<script>alert("unsafe")</script>'
        first = save(first, markdown)
        second = save(second, '# Shared\n\n![same](' + asset["public_url"] + ')')
        denied(admin, f"/api/v1/admin/tutorial-assets/{first['id']}/{asset['id']}", 409, "DELETE")
        denied(admin, f"/api/v1/admin/tutorials/{first['id']}", 409, "PATCH", {"version": first["version"] - 1, "title": "stale"})
        published = admin.json(f"/api/v1/admin/tutorials/{first['id']}/status", "PUT", {"status": "published", "version": first["version"]})
        visible = admin.json(f"/api/v1/tutorials/{first['slug']}")
        check("published HTML is server sanitized", "<script" not in visible["content_html"] and "<img" in visible["content_html"])
        urls = re.findall(r'src="([^"]+)"', visible["content_html"])
        check("published asset URL is signed", bool(urls) and "signature=" in urls[0])
        import html
        raw, headers = anonymous.request(html.unescape(urls[0]), headers={"Range": "bytes=0-15"})
        check("media range responds with correct bytes", len(raw) == 16 and headers.get("Content-Range", "").startswith("bytes 0-15/"))
        first = get_admin(first["id"])
        first = save(first, '# Draft change\n\nUnpublished body\n\n![pixel](' + asset["public_url"] + ')')
        unchanged = admin.json(f"/api/v1/tutorials/{first['slug']}")
        check("saving draft preserves published body", "First published body" in unchanged["content_html"] and "Unpublished body" not in unchanged["content_html"])
        revisions = admin.json(f"/api/v1/admin/tutorials/{first['id']}/revisions")
        check("history records immutable revisions", len(revisions) >= 3)
        original = min(revisions, key=lambda r: r["version"])
        restored = admin.json(f"/api/v1/admin/tutorials/{first['id']}/restore", "POST", {"revision_id": original["id"], "version": first["version"]})["tutorial"]
        check("restoring history creates a new draft version", restored["version"] > first["version"] and "Original" in restored["content_markdown"])
        unchanged = admin.json(f"/api/v1/tutorials/{first['slug']}")
        check("restore leaves published snapshot untouched", "First published body" in unchanged["content_html"])
        admin.json(f"/api/v1/admin/tutorials/{first['id']}/status", "PUT", {"status": "offline", "version": restored["version"]})
        denied(admin, f"/api/v1/tutorials/{first['slug']}", 404)
        # The anonymous endpoint must not reveal private drafts even if global
        # sharing is enabled in another local test.
        denied(anonymous, f"/api/v1/public/tutorials/{second['slug']}", 404)
        write_json(args.output, {"passed": findings, "count": len(findings), "all_passed": True})
        print(json.dumps({"checks_passed": len(findings), "all_passed": True}))
    finally:
        failures = []
        for identity in reversed(ids):
            try:
                current = get_admin(identity)
                admin.json(f"/api/v1/admin/tutorials/{identity}?version={current['version']}", "DELETE")
            except Exception:
                failures.append(identity)
        if failures:
            print("Temporary tutorial cleanup requires review: " + json.dumps(failures), file=sys.stderr)


if __name__ == "__main__":
    main()
