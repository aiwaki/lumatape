import hashlib
import importlib.util
import json
import contextlib
import io
import subprocess
import sys
import tempfile
import unittest
from unittest import mock
from pathlib import Path

spec = importlib.util.spec_from_file_location("licenses", Path(__file__).with_name("licenses.py"))
licenses = importlib.util.module_from_spec(spec)
spec.loader.exec_module(licenses)


class LicenseCollectionTests(unittest.TestCase):
    def test_cli_uses_utf8_when_windows_default_is_cp1252(self):
        # Cargo emits UTF-8 even when Python's Windows text defaults are CP1252.
        # Exercise a real subprocess decode and all collector file I/O under
        # that default, with Unicode descriptions, paths and notice filenames.
        real_run = subprocess.run
        real_open = Path.open

        def windows_open(path, mode="r", buffering=-1, encoding=None, errors=None, newline=None):
            if "b" not in mode and encoding is None:
                encoding = "cp1252"
            return real_open(path, mode, buffering, encoding, errors, newline)

        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "сборка"
            crate = root / "dependency"
            crate.mkdir(parents=True)
            manifest = root / "Cargo.toml"
            manifest.write_bytes(b'[package]\nname="app"\nversion="1.0.0"\n')
            manifest.with_name("Cargo.lock").write_bytes(b"locked fixture\n")
            license_name = "LICENSE-ёж.txt"
            notice = "MIT license — автор\n".encode("utf-8")
            (crate / license_name).write_bytes(notice)
            metadata = {
                "packages": [
                    {"id": "app", "name": "app", "version": "1", "manifest_path": str(manifest)},
                    {"id": "dep", "name": "dep", "version": "1.0.0", "manifest_path": str(crate / "Cargo.toml"),
                     "description": "Русский текст", "repository": "https://example.invalid/ёж", "license": "MIT"},
                ],
                "workspace_members": ["app"],
                "resolve": {"nodes": [{"id": "app", "deps": [{"pkg": "dep"}]}]},
            }
            payload = json.dumps(metadata, ensure_ascii=False).encode("utf-8")

            def cargo_metadata(command, **kwargs):
                self.assertIn("--offline", command)
                return real_run([sys.executable, "-c", f"import sys; sys.stdout.buffer.write({payload!r})"], **kwargs)

            output = root / "лицензии"
            with mock.patch.object(sys, "argv", ["licenses.py", "--manifest", str(manifest), "--output", str(output)]), \
                 mock.patch.object(subprocess, "run", side_effect=cargo_metadata), \
                 mock.patch.object(subprocess, "_text_encoding", return_value="cp1252"), \
                 mock.patch.object(Path, "open", windows_open), contextlib.redirect_stdout(io.StringIO()):
                licenses.main()
            report = json.loads((output / "manifest.json").read_bytes())
            self.assertEqual(report["missing_texts"], [])
            self.assertEqual(report["packages"][0]["repository"], "https://example.invalid/ёж")
            self.assertEqual((output / "dep-1.0.0" / license_name).read_bytes(), notice)
            self.assertIn(license_name, (output / "THIRD-PARTY-NOTICES.txt").read_bytes().decode("utf-8"))

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
