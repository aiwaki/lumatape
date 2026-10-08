"""Verify byte-pinned inputs through a real Git checkout with Windows defaults."""
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]


class LicenseCheckoutTests(unittest.TestCase):
    def test_pinned_inputs_survive_autocrlf_checkout(self):
        expected = {}
        for folder, field, path_key in (
            ("desktop/scripts/license-overrides", "sources", "file"),
            ("desktop/scripts/npm-license-overrides", "texts", "path"),
        ):
            for source in (ROOT / folder).glob("*/provenance.json"):
                for item in json.loads(source.read_bytes())[field]:
                    expected[(source.parent / item[path_key]).relative_to(ROOT).as_posix()] = item["sha256"]
        for package in json.loads((ROOT / "third_party/frontend-provenance.json").read_bytes())["packages"]:
            for item in package["texts"]:
                expected["third_party/" + item["path"]] = item["sha256"]
            if "asset" in package:
                item = package["asset"]
                expected[item["path"]] = item["sha256"]
        self.assertTrue(expected, "Expected pinned upstream inputs")
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            repository, checkout = root / "repository", root / "checkout"
            repository.mkdir()

            def git(*arguments):
                subprocess.run(["git", "-C", str(repository), *arguments], check=True, capture_output=True)

            git("init", "--quiet")
            git("config", "core.autocrlf", "true")
            git("config", "core.safecrlf", "false")
            shutil.copyfile(ROOT / ".gitattributes", repository / ".gitattributes")
            for name, digest in expected.items():
                data = (ROOT / name).read_bytes()
                self.assertEqual(hashlib.sha256(data).hexdigest(), digest, name)
                path = repository / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(data)
            # Upstream bytes can themselves contain CRLF: preservation must not
            # merely rewrite all vendored text to LF to satisfy today's hashes.
            upstream = b"upstream line one\r\nupstream line two\r\n"
            for prefix in ("desktop/scripts/license-overrides", "desktop/scripts/npm-license-overrides", "third_party", "desktop/ui/assets/fonts"):
                name = prefix + "/checkout-fixture/LICENSE"
                path = repository / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(upstream)
                expected[name] = hashlib.sha256(upstream).hexdigest()
            git("add", ".")
            git("checkout-index", "--all", "--prefix=" + checkout.as_posix() + "/")
            for name, digest in expected.items():
                self.assertEqual(hashlib.sha256((checkout / name).read_bytes()).hexdigest(), digest, name)


if __name__ == "__main__":
    unittest.main()
