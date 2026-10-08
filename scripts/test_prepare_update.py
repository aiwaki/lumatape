import base64
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location("prepare_update", Path(__file__).with_name("prepare-update.py"))
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
FIXTURE = json.loads((Path(__file__).resolve().parent.parent / "desktop/src-tauri/tests/fixtures/updater-signed.json").read_text())


class UpdateAdmissionTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.installer = self.root / "LumaTape_0.3.1_x64-setup.exe"
        self.installer.write_bytes(base64.b64decode(FIXTURE["payload"]))
        self.signature = self.root / "installer.sig"
        self.signature.write_text(FIXTURE["signature"])
        self.key = self.root / "public.key"
        self.key.write_text(FIXTURE["public_key"])

    def prepare(self, **overrides):
        arguments = dict(installer=self.installer, signature_file=self.signature, public_key_file=self.key,
                         version="0.3.1", published_at="2026-10-08T00:00:00Z")
        return MODULE.prepare(**(arguments | overrides))

    def test_authentic_payload_produces_immutable_tauri_offer(self):
        manifest, receipt = self.prepare()
        self.assertEqual(manifest["platforms"]["windows-x86_64"]["url"],
                         "https://github.com/aiwaki/lumatape/releases/download/v0.3.1/LumaTape_0.3.1_x64-setup.exe")
        self.assertTrue(receipt["signature_verified"])
        self.assertFalse(receipt["published"])
        self.assertEqual(self.prepare(), (manifest, receipt))

    def test_renamed_signed_old_binary_is_rejected(self):
        renamed = self.root / "LumaTape_99.99.99_x64-setup.exe"
        renamed.write_bytes(self.installer.read_bytes())
        with self.assertRaisesRegex(ValueError, "version does not match"):
            self.prepare(installer=renamed, version="99.99.99")

    def test_structural_resource_parser_rejects_truncation_cycles_and_outside_rvas(self):
        payload = self.installer.read_bytes()
        MODULE.validate_pe_version(payload, "0.3.1")
        import struct
        # Offsets belong to the purpose-built single-section PE fixture.
        for offset, value in [(512+20, 0x80000000), (512+72, 0xffffff00), (152+116, 0xffffffff), (512+96+40, 0)]:
            changed = bytearray(payload)
            struct.pack_into("<I", changed, offset, value)
            with self.subTest(offset=offset), self.assertRaises(ValueError):
                MODULE.validate_pe_version(changed, "0.3.1")
        with self.assertRaises(ValueError):
            MODULE.validate_pe_version(payload[:700], "0.3.1")

    def test_modified_signed_payload_is_rejected(self):
        self.installer.write_bytes(self.installer.read_bytes() + b"tampered")
        with self.assertRaisesRegex(ValueError, "verification failed"):
            self.prepare()

    def test_wrong_signature_and_key_are_rejected(self):
        for target in [self.signature, self.key]:
            with self.subTest(target=target.name):
                saved = target.read_bytes()
                target.write_text("invalid")
                with self.assertRaises(ValueError):
                    self.prepare()
                target.write_bytes(saved)

    def test_portable_and_cross_version_names_are_rejected(self):
        for name in ["lumatape.zip", "LumaTape_0.3.0_x64-setup.exe", "LumaTape_0.3.1_arm64-setup.exe"]:
            path = self.root / name
            path.write_bytes(self.installer.read_bytes())
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, "immutable installer"):
                self.prepare(installer=path)

    def test_invalid_version_dates_symlinks_and_oversize_are_rejected(self):
        for version in ["0.3.1-preview", "0.3.1+local", "0.03.1", "65536.0.0"]:
            with self.subTest(version=version), self.assertRaises(ValueError):
                self.prepare(version=version)
        with self.assertRaises(ValueError):
            self.prepare(published_at="2026-10-08T00:00:00")
        with self.assertRaises(ValueError):
            MODULE.read_regular(self.installer, 1)
        link = self.root / "link"
        link.symlink_to(self.installer)
        with self.assertRaises(ValueError):
            MODULE.read_regular(link, MODULE.LIMIT)


if __name__ == "__main__":
    unittest.main()
