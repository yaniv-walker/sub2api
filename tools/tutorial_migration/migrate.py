#!/usr/bin/env python3
"""Archive a tutorial site and import verified snapshots as editable drafts.

Python standard library only. Credentials are read from environment variables;
archives, reports and import journals never contain them. No command publishes.
"""
from __future__ import annotations

import argparse
import concurrent.futures
import html
import hashlib
import json
import mimetypes
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from datetime import datetime, timezone
from pathlib import Path

ASSET = re.compile(r"/api/v1/tutorial-assets/[a-zA-Z0-9_-]+(?:\.[a-zA-Z0-9]+)?")
SLUG = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
MAX_FILE = 200 * 1024 * 1024
MAX_JSON = 16 * 1024 * 1024


def read_json(path):
    return json.loads(Path(path).read_text(encoding="utf-8-sig"))


def write_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + ".tmp")
    temporary.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    temporary.replace(path)


def validate_base(value):
    url = urllib.parse.urlsplit(value)
    if url.username or url.password or url.query or url.fragment or url.path not in ("", "/"):
        raise ValueError("base URL must be an origin without credentials, query or path")
    if url.scheme != "https" and not (url.scheme == "http" and url.hostname in ("localhost", "127.0.0.1", "::1")):
        raise ValueError("HTTPS is required except for localhost")
    if not url.hostname:
        raise ValueError("base URL requires a host")
    return value.rstrip("/")


class SameOriginRedirect(urllib.request.HTTPRedirectHandler):
    def __init__(self, origin):
        self.origin = origin

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        url = urllib.parse.urlsplit(newurl)
        if f"{url.scheme}://{url.netloc}" != self.origin:
            raise ValueError("cross-origin redirect refused")
        return super().redirect_request(req, fp, code, msg, headers, newurl)


class Client:
    def __init__(self, base, token="", proxy=None):
        self.base = validate_base(base)
        self.token = token
        handlers = [SameOriginRedirect(self.base)]
        # An explicit empty proxy bypasses OS proxy for local acceptance runs.
        if proxy is not None:
            handlers.append(urllib.request.ProxyHandler({"https": proxy, "http": proxy} if proxy else {}))
        self.opener = urllib.request.build_opener(*handlers)

    def request(self, path, method="GET", body=None, headers=None, limit=MAX_JSON):
        if not path.startswith("/") or path.startswith("//") or "\\" in path:
            raise ValueError("request must use an origin-relative path")
        request_headers = {"User-Agent": "Sub2API-Tutorial-Migration/1.0", **(headers or {})}
        if self.token:
            request_headers["Authorization"] = "Bearer " + self.token
        attempts = 3 if method == "GET" else 1
        for attempt in range(attempts):
            try:
                req = urllib.request.Request(self.base + path, data=body, headers=request_headers, method=method)
                with self.opener.open(req, timeout=60) as response:
                    data = response.read(limit + 1)
                    if len(data) > limit:
                        raise ValueError("response exceeds configured size limit")
                    return data, response.headers
            except urllib.error.HTTPError as error:
                # Do not include response bodies, headers or credential-bearing URLs.
                if attempt + 1 >= attempts or error.code < 500:
                    raise RuntimeError(f"HTTP {error.code}: {method} {path.split('?')[0]}") from None
            except (OSError, TimeoutError):
                if attempt + 1 >= attempts:
                    raise RuntimeError(f"connection failed: {method} {path.split('?')[0]}") from None
            time.sleep(0.3 * (attempt + 1))

    def json(self, path, method="GET", value=None):
        body = json.dumps(value).encode("utf-8") if value is not None else None
        raw, _ = self.request(path, method, body, {"Content-Type": "application/json"})
        envelope = json.loads(raw)
        if envelope.get("code") != 0:
            raise RuntimeError(f"API rejected {method} {path.split('?')[0]}")
        return envelope.get("data")

    def upload(self, tutorial_id, path, label):
        boundary = "tutorial-" + uuid.uuid4().hex
        mime = mimetypes.guess_type(path.name)[0] or "application/octet-stream"
        # Archive filenames are hashes with a verified extension, never user input.
        body = (f'--{boundary}\r\nContent-Disposition: form-data; name="label"\r\n\r\n{label}\r\n'
                f'--{boundary}\r\nContent-Disposition: form-data; name="file"; filename="{path.name}"\r\n'
                f'Content-Type: {mime}\r\n\r\n').encode("utf-8") + path.read_bytes() + f"\r\n--{boundary}--\r\n".encode()
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        for attempt in range(3):
            try:
                raw, _ = self.request(f"/api/v1/admin/tutorial-assets/{tutorial_id}", "POST", body,
                                      {"Content-Type": f"multipart/form-data; boundary={boundary}"})
                result = json.loads(raw)
                if result.get("code") != 0:
                    raise RuntimeError("asset upload rejected")
                return result["data"]
            except RuntimeError as error:
                if not str(error).startswith(("connection failed", "HTTP 500:", "HTTP 502:", "HTTP 503:", "HTTP 504:")):
                    raise
                # A lost response does not mean the upload failed. Check linked
                # metadata before retrying the content-addressed, idempotent upload.
                linked = self.json(f"/api/v1/admin/tutorial-assets/{tutorial_id}")
                found = next((asset for asset in linked if asset.get("sha256") == digest), None)
                if found:
                    return found
                if attempt == 2:
                    raise
                time.sleep(0.2 * (attempt + 1))


def validate_documents(documents):
    if not isinstance(documents, list) or not documents:
        raise ValueError("expected a nonempty document array")
    seen_ids, seen_slugs = set(), set()
    for doc in documents:
        if not isinstance(doc, dict) or not SLUG.fullmatch(str(doc.get("slug", ""))):
            raise ValueError("invalid source document slug")
        if not isinstance(doc.get("content"), str) or not isinstance(doc.get("title"), str):
            raise ValueError("source content and title must be strings")
        if not isinstance(doc.get("id"), int) or isinstance(doc["id"], bool) or doc["id"] < 1:
            raise ValueError("invalid source document identity")
        if doc["id"] in seen_ids or doc["slug"] in seen_slugs:
            raise ValueError("duplicate source identity or slug")
        seen_ids.add(doc["id"])
        seen_slugs.add(doc["slug"])


def media_paths(documents):
    # Source API assets have a bounded token path. Include all metadata too.
    return sorted(set(ASSET.findall(json.dumps(documents, ensure_ascii=False))))


def rewrite_media(content, mapping):
    return ASSET.sub(lambda match: mapping.get(match.group(), match.group()), content)


def archive_file(root, relative):
    if not isinstance(relative, str) or "\\" in relative or ":" in relative:
        raise ValueError("unsafe archive path")
    root = Path(root).resolve()
    candidate = (root / relative).resolve()
    if candidate == root or not candidate.is_relative_to(root):
        raise ValueError("archive path escapes its root")
    return candidate


def verify_archive(root, manifest):
    for item in [*manifest.get("documents", []), *manifest.get("media", [])]:
        file = archive_file(root, item["file"])
        if not file.is_file() or file.stat().st_size > MAX_FILE:
            raise ValueError("missing or oversized archive file")
        data = file.read_bytes()
        if hashlib.sha256(data).hexdigest() != item["sha256"]:
            raise ValueError("archive checksum mismatch: " + item["file"])
        if item.get("size_bytes", len(data)) != len(data):
            raise ValueError("archive file length mismatch")


def review_document(doc, origin):
    text = doc.get("content", "") + doc.get("title", "") + doc.get("summary", "")
    reasons = []
    if origin in text:
        reasons.append("source_domain")
    if re.search(r"微信|技术QQ|售后|客服", text):
        reasons.append("support_details")
    if re.search(r"价格|兑换|折|退款|充值|封号|人民币", text):
        reasons.append("commercial_terms")
    if ASSET.search(text):
        reasons.append("screenshots_and_recordings")
    return {"slug": doc["slug"], "reasons": reasons, "status": "needs_review" if reasons else "ready"}


def snapshot(args):
    root = Path(args.archive).resolve()
    root.mkdir(parents=True, exist_ok=True)
    client = Client(args.source, os.getenv(args.source_token_env, ""), args.proxy)
    if args.source_json:
        response = read_json(args.source_json)
        documents = response["data"] if isinstance(response, dict) else response
    else:
        documents = client.json("/api/v1/tutorials")
    validate_documents(documents)
    manifest_path = root / "manifest.json"
    if manifest_path.exists():
        previous = read_json(manifest_path)
        if previous["source_origin"] != client.base:
            raise ValueError("archive belongs to another source")
        verify_archive(root, previous)
        previous_docs = [read_json(archive_file(root, d["file"])) for d in previous["documents"]]
        if previous_docs != documents:
            raise ValueError("source changed; create a new snapshot directory to preserve the old snapshot")
        print(json.dumps({"status": "already_archived", "documents": len(documents), "media": len(previous["media"])}))
        return
    manifest = {"format": 1, "source_origin": client.base, "captured_at": datetime.now(timezone.utc).isoformat(), "documents": [], "media": []}
    for doc in documents:
        relative = f"documents/{doc['slug']}.json"
        file = archive_file(root, relative)
        write_json(file, doc)
        data = file.read_bytes()
        manifest["documents"].append({"source_id": str(doc["id"]), "slug": doc["slug"], "file": relative, "sha256": hashlib.sha256(data).hexdigest(), "size_bytes": len(data)})

    partial_path = root / "download-progress.json"
    partial = read_json(partial_path) if partial_path.exists() else {}

    def download(path):
        if path in partial:
            entry = partial[path]
            verify_archive(root, {"media": [entry]})
            return entry
        raw, headers = client.request(path, limit=MAX_FILE)
        mime = headers.get_content_type()
        signatures = {"image/png": (b"\x89PNG\r\n\x1a\n", ".png"), "image/jpeg": (b"\xff\xd8\xff", ".jpg"),
                      "image/gif": (b"GIF8", ".gif"), "image/webp": (b"RIFF", ".webp")}
        if mime in signatures:
            signature, extension = signatures[mime]
            if not raw.startswith(signature):
                raise ValueError("source media signature does not match MIME")
        elif mime == "video/mp4" and raw[4:8] == b"ftyp":
            extension = ".mp4"
        elif mime == "video/webm" and raw.startswith(b"\x1a\x45\xdf\xa3"):
            extension = ".webm"
        else:
            raise ValueError("unsupported source media format")
        digest = hashlib.sha256(raw).hexdigest()
        relative = f"media/{digest}{extension}"
        destination = archive_file(root, relative)
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(raw)
        return {"source_path": path, "file": relative, "sha256": digest, "size_bytes": len(raw), "mime_type": mime}

    errors = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
        futures = {pool.submit(download, path): path for path in media_paths(documents)}
        for future in concurrent.futures.as_completed(futures):
            try:
                item = future.result()
                manifest["media"].append(item)
                partial[item["source_path"]] = item
                write_json(partial_path, partial)
                print(json.dumps({"downloaded": len(manifest["media"]), "bytes": item["size_bytes"]}), flush=True)
            except (ValueError, RuntimeError, OSError) as error:
                errors.append(str(error))
    if errors:
        raise RuntimeError(f"{len(errors)} media downloads failed; rerun snapshot to resume verified files. " + errors[0])
    manifest["media"].sort(key=lambda item: item["source_path"])
    write_json(root / "review.json", [review_document(doc, client.base) for doc in documents])
    verify_archive(root, manifest)
    write_json(manifest_path, manifest)
    print(json.dumps({"status": "archived", "documents": len(documents), "media": len(manifest["media"]), "bytes": sum(x["size_bytes"] for x in manifest["media"])}))


def import_payload(doc, origin, content):
    return {"slug": doc["slug"], "title": doc["title"], "summary": doc.get("summary", ""), "category": doc.get("category", "getting-started"),
            "content_markdown": content, "content_html": "", "is_public": False, "sort_order": doc.get("sort_order", 100),
            "resources": doc.get("resources") or [], "source_origin": origin, "source_document_id": str(doc["id"]),
            "source_updated_at": doc.get("updated_at"), "source_relation": doc.get("source_url", "")}


def rebrand_document(doc, profile, origin):
    """Explicit editorial adaptation; original snapshots are never rewritten."""
    name = str(profile["site_name"]).strip()
    base = validate_base(profile["api_base"])
    qq, group = str(profile.get("qq", "")), str(profile.get("qq_group", ""))
    if not name or len(name) > 100 or not re.fullmatch(r"\d{5,15}", qq) or not re.fullmatch(r"\d{5,15}", group):
        raise ValueError("profile requires site_name, api_base, numeric qq and qq_group")
    result = json.loads(json.dumps(doc))
    support = f"联系 {name} 售后：QQ 群 **{group}**，个人 QQ **{qq}**。请说明使用的客户端、报错信息及发生时间；不要发送完整 API Key。"
    content = doc["content"]
    # These exact source callouts carry marketplace contact details, prices and
    # guarantees; retain the originals in the immutable archive, not as our policy.
    callout = re.compile(r'<div\s+data-callout="(cyan|green|rose)"[^>]*>.*?</div>', re.S)
    def adapt_block(match):
        block = match.group()
        if "laiqi813" in block or "3223114951" in block:
            return '<div data-callout="cyan"><p>' + html.escape(support.replace("**", "")) + "</p></div>"
        if match.group(1) == "green" and ("价格" in block or "兑换" in block):
            return '<div data-callout="green"><p>模型、可用分组、计费与充值规则以本站页面展示为准。</p></div>'
        if match.group(1) == "rose" and ("封号" in block or "核心优势" in block):
            return '<div data-callout="rose"><p>请按本文配置客户端。可用模型、配额与服务范围以本站页面及实际服务规则为准。</p></div>'
        return block
    content = callout.sub(adapt_block, content)
    content = re.sub(r"(?ms)^# 获取支持\s*\n.*?(?=^# |\Z)", "# 获取支持\n\n" + support + "\n\n", content)
    content = re.sub(r"微信\s*laiqi813\s*/\s*QQ\s*3223114951", f"QQ 群 {group} / 个人 QQ {qq}", content)
    # Replace only the known source identity; screenshot pixels remain originals.
    for field in ("title", "summary"):
        result[field] = re.sub(r"CODELINK|CodeLink|Codelink", lambda _: name, str(result.get(field, "")))
    content = re.sub(r"CODELINK|CodeLink|Codelink", lambda _: name, content)
    content = content.replace(origin, base)
    content = re.sub(r"\bcodelink\b", "relaygateway", content)
    # Repair known malformed original hrefs that included Chinese parentheticals.
    content = re.sub(r"\]\(" + re.escape(base) + r"%EF%BC%88[^)]+\)", "](" + base + ")", content)
    content = content.replace("目前仅支持 QQ 邮箱和 163 邮箱注册", "可用邮箱类型以本站注册页面为准")
    content = content.replace("（充值后所有模型皆可调用）", "（可用模型以密钥所属分组为准）")
    result["content"] = content
    return result


def target_client(args):
    token = os.getenv(args.target_token_env, "")
    if not token:
        raise ValueError(f"set {args.target_token_env} for target administrator access")
    return Client(args.target, token, "" if urllib.parse.urlsplit(args.target).hostname in ("localhost", "127.0.0.1", "::1") else args.proxy)


def admin_documents(client):
    items, page = [], 1
    while True:
        data = client.json(f"/api/v1/admin/tutorials?page={page}&page_size=100")
        items.extend(data["items"])
        if len(items) >= data["total"] or not data["items"]:
            return items
        page += 1


def apply_import(args):
    root = Path(args.archive).resolve()
    manifest = read_json(root / "manifest.json")
    verify_archive(root, manifest)
    documents = [read_json(archive_file(root, x["file"])) for x in manifest["documents"]]
    validate_documents(documents)
    profile = read_json(args.profile) if getattr(args, "profile", None) else None
    if profile:
        documents = [rebrand_document(doc, profile, manifest["source_origin"]) for doc in documents]
    client = target_client(args)
    existing = admin_documents(client)
    by_source = {(x.get("source_origin"), str(x.get("source_document_id"))): x for x in existing if x.get("source_document_id")}
    by_slug = {x["slug"]: x for x in existing}
    journal_path = root / ("import-" + hashlib.sha256(client.base.encode()).hexdigest()[:12] + ".json")
    journal = read_json(journal_path) if journal_path.exists() else {"batch_id": str(uuid.uuid4()), "target_origin": client.base, "documents": {}, "media": {}}
    if journal["target_origin"] != client.base:
        raise ValueError("journal target mismatch")
    rolled_back_journal = journal if journal.get("rolled_back_at") else None
    if rolled_back_journal:
        journal = {"batch_id": str(uuid.uuid4()), "target_origin": client.base, "documents": {}, "media": {}}
    actions = []
    for doc in documents:
        key = str(doc["id"])
        found = by_source.get((manifest["source_origin"], key))
        pending = journal["documents"].get(key)
        action = "resume_draft" if found and pending and not pending.get("complete") else "skip_existing_source" if found else "conflict" if doc["slug"] in by_slug else "create_draft"
        actions.append({"source_id": key, "slug": doc["slug"], "action": action})
    print(json.dumps({"dry_run": not args.apply, "actions": actions}, ensure_ascii=False), flush=True)
    if not args.apply:
        return
    if any(x["action"] == "conflict" for x in actions):
        raise ValueError("slug conflict: no documents changed; resolve mapping before import")
    if rolled_back_journal:
        previous_batch_id = str(uuid.UUID(rolled_back_journal["batch_id"]))
        write_json(root / (journal_path.stem + "-rolled-back-" + previous_batch_id + ".json"), rolled_back_journal)
    for doc, action in zip(documents, actions):
        key = str(doc["id"])
        saved = journal["documents"].get(key)
        found = by_source.get((manifest["source_origin"], key))
        if found and not saved:
            continue  # Never overwrite source-linked content imported by another batch.
        if saved and saved.get("complete"):
            continue  # Manual edits after import are deliberately preserved.
        if saved:
            result = client.json(f"/api/v1/admin/tutorials/{saved['id']}")
            current = result["tutorial"]
            if current["version"] != saved["version"]:
                raise ValueError("draft changed since interrupted import; manual review required")
        else:
            if found:
                raise ValueError("existing source cannot be adopted automatically")
            # Source identity is recorded at creation so a lost response cannot duplicate it.
            current = client.json("/api/v1/admin/tutorials", "POST", import_payload(doc, manifest["source_origin"], "# 导入中\n\n此草稿尚未完成媒体关联，请勿发布。"))
            saved = {"id": current["id"], "slug": current["slug"], "version": current["version"], "complete": False}
            journal["documents"][key] = saved
            write_json(journal_path, journal)
        # Archive all originals into the first document's asset library, including
        # removed source QR codes; only the reviewed text determines published refs.
        refs = set(ASSET.findall(json.dumps(doc, ensure_ascii=False)))
        if doc is documents[0]:
            refs.update(asset["source_path"] for asset in manifest["media"])
        for asset in manifest["media"]:
            source = asset["source_path"]
            if source not in refs or source in journal["media"]:
                continue
            uploaded = client.upload(current["id"], archive_file(root, asset["file"]), "导入素材 " + Path(asset["file"]).stem[:12])
            journal["media"][source] = {"url": uploaded["public_url"], "id": uploaded["id"], "sha256": asset["sha256"]}
            write_json(journal_path, journal)
        mapping = {source: data["url"] for source, data in journal["media"].items()}
        payload = import_payload(doc, manifest["source_origin"], rewrite_media(doc["content"], mapping))
        payload["resources"] = json.loads(rewrite_media(json.dumps(payload["resources"], ensure_ascii=False), mapping))
        payload["version"] = current["version"]
        result = client.json(f"/api/v1/admin/tutorials/{current['id']}", "PATCH", payload)
        updated = result.get("tutorial", result)
        saved.update(version=updated["version"], complete=True)
        write_json(journal_path, journal)
        print(json.dumps({"imported": doc["slug"], "status": "draft", "id": current["id"]}), flush=True)


def rollback(args):
    journal = read_json(args.journal)
    client = target_client(args)
    if journal["target_origin"] != client.base:
        raise ValueError("journal target mismatch")
    # Inspect the whole batch first. Never partly delete a batch after a conflict.
    current = admin_documents(client)
    by_id = {doc["id"]: doc for doc in current}
    targets = []
    for source_id, saved in journal["documents"].items():
        doc = by_id.get(saved["id"])
        if not doc:
            continue
        if doc["version"] != saved["version"] or doc["status"] != "draft" or doc["slug"] != saved["slug"] or str(doc.get("source_document_id")) != source_id:
            raise ValueError("batch contains edited or published documents; rollback refused")
        targets.append(doc)
    print(json.dumps({"dry_run": not args.apply, "delete_drafts": [d["slug"] for d in targets]}), flush=True)
    if args.apply:
        for doc in targets:
            client.json(f"/api/v1/admin/tutorials/{doc['id']}?version={doc['version']}", "DELETE")
        journal["rolled_back_at"] = datetime.now(timezone.utc).isoformat()
        write_json(args.journal, journal)
        # Physical blobs are deliberately retained: reference-aware garbage collection
        # can reclaim them later without destroying shared or historical resources.


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    subs = parser.add_subparsers(dest="command", required=True)
    capture = subs.add_parser("snapshot", help="download immutable document and media archive")
    capture.add_argument("--source", default="https://api.codelink.chat")
    capture.add_argument("--source-json", help="reuse a previously captured list response")
    capture.add_argument("--source-token-env", default="TUTORIAL_SOURCE_TOKEN")
    capture.add_argument("--archive", required=True)
    capture.add_argument("--proxy", default=None)
    capture.set_defaults(run=snapshot)
    verify = subs.add_parser("verify", help="verify all local archive hashes")
    verify.add_argument("--archive", required=True)
    verify.set_defaults(run=lambda a: (verify_archive(Path(a.archive), read_json(Path(a.archive) / "manifest.json")), print("Archive verified")))
    for name, function in (("import", apply_import), ("rollback", rollback)):
        command = subs.add_parser(name)
        command.add_argument("--target", required=True)
        command.add_argument("--target-token-env", default="TUTORIAL_TARGET_TOKEN")
        command.add_argument("--proxy", default=None)
        command.add_argument("--apply", action="store_true", help="perform writes; omitted is read-only dry run")
        command.add_argument("--archive" if name == "import" else "--journal", required=True)
        if name == "import":
            command.add_argument("--profile", help="explicit site_name/api_base/qq/qq_group editorial profile JSON")
        command.set_defaults(run=function)
    args = parser.parse_args()
    try:
        args.run(args)
    except (ValueError, RuntimeError, OSError, KeyError, json.JSONDecodeError) as error:
        print("Migration stopped: " + str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
