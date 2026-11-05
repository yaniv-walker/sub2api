"""Migration regressions; no network or user credentials required."""
import hashlib
import json
import tempfile
from argparse import Namespace
import unittest
from pathlib import Path
from unittest.mock import patch

import migrate


class MigrationTests(unittest.TestCase):
    def test_collects_markdown_html_and_shared_media_once(self):
        documents = [dict(id=1, slug="guide", title="Guide", content='![a](/api/v1/tutorial-assets/abc) <video src="/api/v1/tutorial-assets/def"></video>'),
                     dict(id=2, slug="client", title="Client", content='<img src="/api/v1/tutorial-assets/abc">')]
        self.assertEqual(migrate.media_paths(documents), ["/api/v1/tutorial-assets/abc", "/api/v1/tutorial-assets/def"])

    def test_rewrites_exact_asset_tokens_not_prefix_collisions(self):
        source = '![a](/api/v1/tutorial-assets/abc) <img src="/api/v1/tutorial-assets/abc-long">'
        self.assertEqual(migrate.rewrite_media(source, {"/api/v1/tutorial-assets/abc": "/api/v1/tutorial-assets/local"}),
                         '![a](/api/v1/tutorial-assets/local) <img src="/api/v1/tutorial-assets/abc-long">')

    def test_archive_paths_cannot_escape(self):
        with tempfile.TemporaryDirectory() as directory:
            for candidate in ("../secret", "/tmp/file", "C:\\secret", "media/../../secret"):
                with self.subTest(candidate=candidate), self.assertRaises(ValueError):
                    migrate.archive_file(Path(directory), candidate)

    def test_archive_detects_file_tampering_before_import(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "media").mkdir()
            (root / "media" / "a.png").write_bytes(b"changed")
            manifest = {"documents": [], "media": [{"file": "media/a.png", "sha256": hashlib.sha256(b"original").hexdigest(), "size_bytes": 8}]}
            with self.assertRaisesRegex(ValueError, "checksum"):
                migrate.verify_archive(root, manifest)

    def test_origin_redirect_cannot_leak_bearer(self):
        import urllib.request
        redirect = migrate.SameOriginRedirect("https://api.codelink.chat")
        request = urllib.request.Request("https://api.codelink.chat/a", headers={"Authorization": "Bearer private"})
        with self.assertRaisesRegex(ValueError, "redirect"):
            redirect.redirect_request(request, None, 302, "", {}, "https://elsewhere.test/file")

    def test_target_http_only_allowed_on_loopback(self):
        self.assertEqual(migrate.validate_base("http://127.0.0.1:18088"), "http://127.0.0.1:18088")
        with self.assertRaises(ValueError):
            migrate.validate_base("http://external.test")

    def test_source_metadata_remains_traceable(self):
        doc = dict(id=3, slug="codex", title="Codex", content="Body", source_url="section:full#Codex", updated_at="2026-11-02T00:00:00Z")
        item = migrate.import_payload(doc, "https://api.codelink.chat", "Body")
        self.assertEqual(item["source_document_id"], "3")
        self.assertEqual(item["source_relation"], "section:full#Codex")
        self.assertFalse(item["is_public"])

    def test_review_finds_source_domain_and_support(self):
        review = migrate.review_document(dict(slug="guide", content='API https://api.codelink.chat 技术微信：laiqi813'), "https://api.codelink.chat")
        self.assertIn("source_domain", review["reasons"])
        self.assertIn("support_details", review["reasons"])

    def test_snapshot_rejects_duplicate_source_identity(self):
        with self.assertRaisesRegex(ValueError, "duplicate"):
            migrate.validate_documents([dict(id=1, slug="a", content="a", title="A"), dict(id=1, slug="b", content="b", title="B")])

    def test_second_import_preserves_manual_edits(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            doc = dict(id=3, slug="codex", title="Codex", content="Body")
            migrate.write_json(root / "documents/codex.json", doc)
            data = (root / "documents/codex.json").read_bytes()
            migrate.write_json(root / "manifest.json", dict(source_origin="https://api.codelink.chat", documents=[dict(file="documents/codex.json", sha256=hashlib.sha256(data).hexdigest())], media=[]))
            class FakeClient:
                base = "http://127.0.0.1:18088"
                docs = []
                writes = 0

                def json(self, path, method="GET", value=None):
                    if method == "GET":
                        return dict(items=self.docs, total=len(self.docs))
                    self.writes += 1
                    if method == "POST":
                        self.docs.append(dict(value, id=1, version=1))
                        return self.docs[0]
                    self.docs[0] = dict(value, id=1, version=2)
                    return dict(tutorial=self.docs[0])

            client = FakeClient()
            args = Namespace(archive=directory, apply=True)
            with patch.object(migrate, "target_client", return_value=client):
                migrate.apply_import(args)
                client.docs[0].update(content_markdown="Human edit", version=7)
                migrate.apply_import(args)
            self.assertEqual(client.writes, 2)
            self.assertEqual(client.docs[0]["content_markdown"], "Human edit")

    def test_snapshot_resumes_only_verified_cached_downloads(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "source.json"
            migrate.write_json(source, [dict(id=1, slug="guide", title="Guide", content='![a](/api/v1/tutorial-assets/abc)')])
            data = b"\x89PNG\r\n\x1a\nexample"
            file = root / "media/a.png"
            file.parent.mkdir()
            file.write_bytes(data)
            migrate.write_json(root / "download-progress.json", {"/api/v1/tutorial-assets/abc": dict(source_path="/api/v1/tutorial-assets/abc", file="media/a.png", sha256=hashlib.sha256(data).hexdigest(), size_bytes=len(data), mime_type="image/png")})
            args = Namespace(archive=directory, source="https://api.codelink.chat", source_json=source, source_token_env="UNUSED_TEST_TOKEN", proxy=None)
            with patch.object(migrate.Client, "request", side_effect=AssertionError("must not download")):
                migrate.snapshot(args)
            self.assertEqual(len(migrate.read_json(root / "manifest.json")["media"]), 1)

    def test_rebrand_replaces_support_without_copying_source_promises(self):
        profile = dict(site_name="RelayGateway（星枢）", api_base="https://relay-gateway.xyz", qq="670188946", qq_group="1106321467")
        source = '<div data-callout="cyan"><p>技术微信：laiqi813</p></div>\n\n<div data-callout="green"><p>1人民币兑换1美刀</p></div>\n\n# 获取支持\n\n**技术QQ：3223114951**\n\n# 注册\n\n访问 https://api.codelink.chat/v1\n'
        doc = dict(id=1, slug="guide", title="CODELINK 帮助", content=source)
        result = migrate.rebrand_document(doc, profile, "https://api.codelink.chat")
        self.assertEqual(result["title"], "RelayGateway（星枢） 帮助")
        self.assertIn("1106321467", result["content"])
        self.assertIn("670188946", result["content"])
        self.assertIn("https://relay-gateway.xyz/v1", result["content"])
        for old in ("laiqi813", "3223114951", "1人民币兑换1美刀", "api.codelink.chat"):
            self.assertNotIn(old, result["content"])
        self.assertIn("laiqi813", doc["content"], "original snapshot must stay immutable")

    def test_upload_recovers_lost_response_by_verified_hash(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "a.png"
            path.write_bytes(b"example")
            client = migrate.Client("http://127.0.0.1:18088", proxy="")
            asset = dict(id=4, sha256=hashlib.sha256(b"example").hexdigest(), public_url="/api/v1/tutorial-assets/local")
            with patch.object(client, "request", side_effect=RuntimeError("connection failed")), patch.object(client, "json", return_value=[asset]):
                self.assertEqual(client.upload(1, path, "example"), asset)

    def test_rollback_checks_entire_batch_before_any_delete(self):
        with tempfile.TemporaryDirectory() as directory:
            file = Path(directory) / "journal.json"
            migrate.write_json(file, {"target_origin": "http://127.0.0.1:18088", "documents": {
                "3": {"id": 1, "slug": "first", "version": 2},
                "4": {"id": 2, "slug": "second", "version": 2}}})
            from unittest.mock import Mock
            client = Mock(base="http://127.0.0.1:18088")
            docs = [dict(id=1, slug="first", version=2, status="draft", source_document_id="3"),
                    dict(id=2, slug="second", version=3, status="draft", source_document_id="4")]
            with patch.object(migrate, "target_client", return_value=client), patch.object(migrate, "admin_documents", return_value=docs):
                with self.assertRaisesRegex(ValueError, "rollback refused"):
                    migrate.rollback(Namespace(journal=file, apply=True))
            client.json.assert_not_called()


if __name__ == "__main__":
    unittest.main()
