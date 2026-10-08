import hashlib
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location("licenses", Path(__file__).with_name("licenses.py"))
licenses = importlib.util.module_from_spec(spec)
spec.loader.exec_module(licenses)


class LicenseCollectionTests(unittest.TestCase):
    def test_reachable_graph_keeps_dual_nested_texts_and_flags_missing(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            for name in ["root", "dual", "missing", "irrelevant"]:
                (root / name).mkdir()
            (root / "dual/LICENSE-MIT").write_text("MIT full text\n")
            (root / "dual/LICENSE-APACHE").write_text("Apache full text\n")
            (root / "dual/licenses/vendor").mkdir(parents=True)
            (root / "dual/licenses/vendor/terms.txt").write_text("Vendor full text\n")
            (root / "irrelevant/LICENSE").write_text("Not a Windows dependency\n")
            packages = [{"id": n, "name": n, "version": "1.0.0", "manifest_path": str(root/n/"Cargo.toml"), "license": "MIT OR Apache-2.0"} for n in ["root", "dual", "missing", "irrelevant"]]
            data = {"packages": packages, "workspace_members": ["root"], "resolve": {"nodes": [{"id": "root", "deps": [{"pkg": "dual"}, {"pkg": "missing"}]}]}}
            report = licenses.collect(data, root/"out")
            self.assertEqual(report["package_count"], 2)
            self.assertEqual(report["missing_texts"], ["missing-1.0.0"])
            self.assertEqual(len(report["packages"][0]["texts"]), 3)
            self.assertNotIn(str(root), json.dumps(report))
            self.assertEqual((root/"out/dual-1.0.0/LICENSE-MIT").read_text(), "MIT full text\n")

    def test_supplement_requires_exact_source_revision_and_content(self):
        with tempfile.TemporaryDirectory() as temp:
            base = Path(temp)
            crate = base/"crate"
            crate.mkdir()
            (crate/".cargo_vcs_info.json").write_text(json.dumps({"git":{"sha1":"abc123"}}))
            override = base/"overrides/sample-1.0.0"
            override.mkdir(parents=True)
            data = b"Actual upstream license text\n"
            (override/"LICENSE").write_bytes(data)
            provenance = {"crate":"sample-1.0.0", "commit":"abc123", "sources":[{"file":"LICENSE", "url":"https://example.invalid/pinned/LICENSE", "sha256":hashlib.sha256(data).hexdigest()}]}
            (override/"provenance.json").write_text(json.dumps(provenance))
            packages = [{"id":"root", "name":"root", "version":"1", "manifest_path":str(base/"Cargo.toml")}, {"id":"sample", "name":"sample", "version":"1.0.0", "manifest_path":str(crate/"Cargo.toml")}]
            metadata = {"packages":packages,"workspace_members":["root"],"resolve":{"nodes":[{"id":"root","deps":[{"pkg":"sample"}]}]}}
            report = licenses.collect(metadata, base/"out", base/"overrides")
            self.assertEqual(report["missing_texts"], [])
            self.assertEqual((base/"out/sample-1.0.0/LICENSE").read_bytes(), data)
            (override/"LICENSE").write_text("tampered")
            with self.assertRaisesRegex(ValueError, "checksum"):
                licenses.collect(metadata, base/"out2", base/"overrides")
            (override/"LICENSE").write_bytes(data)
            provenance["commit"] = "different"
            (override/"provenance.json").write_text(json.dumps(provenance))
            with self.assertRaisesRegex(ValueError, "published source"):
                licenses.collect(metadata, base/"out3", base/"overrides")

    def test_declared_file_and_symlink_boundary(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root/"crate").mkdir()
            (root/"crate/terms.txt").write_text("Declared license\n")
            (root/"outside").write_text("Not package text")
            (root/"crate/LICENSE").symlink_to(root/"outside")
            self.assertEqual(licenses.license_files(root/"crate", "terms.txt"), [root/"crate/terms.txt"])
            self.assertEqual(licenses.license_files(root/"crate", "../outside"), [])


if __name__ == "__main__":
    unittest.main()
