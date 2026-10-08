import argparse
import contextlib
import hashlib
import importlib.util
import io
import json
import os
import subprocess
import struct
import tempfile
import unittest
import zipfile
from pathlib import Path

spec = importlib.util.spec_from_file_location('package_desktop', Path(__file__).with_name('package-desktop.py'))
package_desktop = importlib.util.module_from_spec(spec)
spec.loader.exec_module(package_desktop)


class DesktopPackageTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = (Path(self.temp.name) / 'repo').resolve()
        self.root.mkdir()
        previous = package_desktop.ROOT
        package_desktop.ROOT = self.root
        self.addCleanup(setattr, package_desktop, 'ROOT', previous)
        for name in ['LICENSE', 'README.md', 'README.en.md', 'START-HERE.txt', 'START-HERE.en.txt', 'assets/lumatape.svg', 'assets/lumatape.ico',
                     'third_party/README.md', 'third_party/README.en.md', 'desktop/src-tauri/README.md', 'desktop/src-tauri/README.en.md', 'desktop/src-tauri/Cargo.lock',
                     'cmd/lumatape-testcard/README.md', 'cmd/lumatape-testcard/README.en.md', 'internal/control/README.md', 'internal/control/README.en.md', 'internal/display/README.md', 'internal/display/README.en.md', 'internal/pointer/README.md', 'internal/pointer/README.en.md', 'native/capture/README.md', 'native/capture/README.en.md', 'scripts/windows-control-smoke/README.md', 'scripts/windows-control-smoke/README.en.md', 'scripts/windows-ui-smoke/README.md', 'scripts/windows-ui-smoke/README.en.md', 'desktop/src-tauri/icons/icon.png', 'desktop/src-tauri/updater.pub',
                     'desktop/package-lock.json', 'third_party/frontend-provenance.json']:
            self.write(name, b'test notice\n')
        for name in ['README.md', 'README.en.md', 'shaders/README.md', 'shaders/README.en.md', 'WINDOWS_VALIDATION.md', 'WINDOWS_VALIDATION.en.md', 'PARALLELS_SMOKE.md', 'PARALLELS_SMOKE.en.md', 'CURRENT_STATE.md', 'ARCHITECTURE.md', 'ARCHITECTURE.en.md', 'PRIOR_ART_AUDIT.md', 'PRIOR_ART_AUDIT.en.md', 'SHADER_SPEC.md', 'SHADER_SPEC.en.md', 'UPDATES.md', 'UPDATES.en.md']:
            self.write('docs/' + name, b'Unqualified test fixture\n')
        for name in ['amber-crt.lumatape.glsl', 'cold-bleed.lumatape.glsl']:
            self.write('examples/shaders/' + name, b'Test shader fixture\n')
        pe = bytearray(256)
        pe[:2] = b'MZ'
        struct.pack_into('<I', pe, 0x3c, 128)
        pe[128:132] = b'PE\0\0'
        struct.pack_into('<H', pe, 132, 0x8664)
        struct.pack_into('<H', pe, 128 + 24 + 68, 2)
        self.shell = self.write('input/lumatape.exe', pe)
        for name in package_desktop.ENGINE:
            self.write('input/engine/' + name, pe)
        self.capture_pe(package_desktop.CAPTURE_EXPORTS)
        for name in ['GLFW-LICENSE.md', 'Go-LICENSE.txt', 'cppwinrt-LICENSE.txt', 'MSVC-STL-LICENSE.txt']:
            self.write('input/licenses/native/' + name, b'Native license\n')
        text = b'Rust dependency license\n'
        self.write('input/licenses/rust/sample-1.0/LICENSE', text)
        manifest = {'package_count': 1, 'missing_texts': [],
                    'cargo_lock_sha256': hashlib.sha256((self.root/'desktop/src-tauri/Cargo.lock').read_bytes()).hexdigest(),
                    'packages': [{'texts': [{'path': 'sample-1.0/LICENSE', 'sha256': hashlib.sha256(text).hexdigest()}]}]}
        self.write('input/licenses/rust/manifest.json', json.dumps(manifest).encode())
        npm_text = b'Frontend runtime license\n'
        vendor_text = b'Full shadcn MIT license\n'
        self.write('input/licenses/npm/packages/react/LICENSE', npm_text)
        self.write('input/licenses/npm/vendored/shadcn/LICENSE', vendor_text)
        npm_manifest = {'package_count': 1, 'missing_texts': [],
                        'package_lock_sha256': hashlib.sha256((self.root/'desktop/package-lock.json').read_bytes()).hexdigest(),
                        'vendor_manifest_sha256': hashlib.sha256((self.root/'third_party/frontend-provenance.json').read_bytes()).hexdigest(),
                        'packages': [{'name': 'react', 'texts': [{'path': 'packages/react/LICENSE', 'sha256': hashlib.sha256(npm_text).hexdigest()}]}],
                        'vendored': [{'name': 'shadcn/ui', 'texts': [{'path': 'vendored/shadcn/LICENSE', 'sha256': hashlib.sha256(vendor_text).hexdigest()}]}]}
        self.write('input/licenses/npm/manifest.json', json.dumps(npm_manifest).encode())
        self.args = argparse.Namespace(shell=self.shell, engine=self.root/'input/engine',
                                      licenses=self.root/'input/licenses', output=None,
                                      version='0.2.0', revision='test', build_time='unknown')

    def write(self, name, data):
        path = self.root/name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
        return path

    def run_package(self):
        with contextlib.redirect_stdout(io.StringIO()) as output:
            package_desktop.package(self.args)
        return json.loads(output.getvalue())

    def capture_pe(self, exports):
        names = sorted(exports)
        pe = bytearray(2048)
        pe[:2] = b'MZ'
        struct.pack_into('<I', pe, 0x3c, 128)
        pe[128:132] = b'PE\0\0'
        struct.pack_into('<HH', pe, 132, 0x8664, 1)
        struct.pack_into('<H', pe, 148, 240)
        struct.pack_into('<H', pe, 152, 0x20b)
        struct.pack_into('<I', pe, 152+60, 512)
        struct.pack_into('<I', pe, 152+108, 16)
        struct.pack_into('<II', pe, 152+112, 0x1000, 384)
        struct.pack_into('<IIII', pe, 392+8, 1536, 0x1000, 1536, 512)
        struct.pack_into('<IIIII', pe, 512+20, len(names), len(names), 0x1040, 0x1080, 0x10c0)
        text = 768
        for i, name in enumerate(names):
            struct.pack_into('<I', pe, 576+i*4, 0x1500)
            struct.pack_into('<I', pe, 640+i*4, 0x1000+text-512)
            struct.pack_into('<H', pe, 704+i*2, i)
            encoded = name.encode('ascii') + b'\0'
            pe[text:text+len(encoded)] = encoded
            text += len(encoded)
        return self.write('input/engine/lumatape_capture.dll', pe)

    def test_channel_metadata_matches_explicit_build_input_in_archive_and_manifest(self):
        for channel in ['unconfigured', 'configured']:
            with self.subTest(channel=channel):
                self.args.update_channel = channel
                result = self.run_package()
                archive = Path(result['archive'])
                prefix = archive.stem + '/'
                with zipfile.ZipFile(archive) as zipped:
                    raw = zipped.read(prefix + 'BUILD-STATUS.json')
                    status = json.loads(raw)
                    self.assertEqual(status['update_channel'], channel)
                    self.assertTrue(status['updater'].startswith(channel + ';'))
                    if channel == 'configured':
                        self.assertIn('checks enabled', status['updater'])
                        self.assertIn('installation disabled for this portable ZIP', status['updater'])
                    else:
                        self.assertIn('no automatic network access', status['updater'])
                    records = json.loads(zipped.read(prefix + 'SOURCE-FILES.json'))
                    record = next(record for record in records if record['name'] == 'BUILD-STATUS.json')
                    self.assertEqual(record['sha256'], hashlib.sha256(raw).hexdigest())
                    self.assertIn(prefix + 'docs/UPDATES.md', zipped.namelist())

    def test_public_support_excludes_local_checkpoint_and_unlisted_documents(self):
        private = b'PRIVATE_LOCAL_CHECKPOINT_SENTINEL'
        self.write('docs/CURRENT_STATE.md', private)
        self.write('docs/local-session.md', private)
        self.write('artifacts/session.log', private)
        support = self.root/'installer-support'
        package_desktop.copy_support_files(support, self.args.licenses)
        self.assertTrue((support/'LICENSE').is_file())
        for path in ['README.en.md', 'START-HERE.en.txt', 'docs/README.md', 'docs/README.en.md', 'docs/shaders/README.md', 'docs/shaders/README.en.md', 'internal/pointer/README.en.md', 'native/capture/README.md', 'third_party/README.en.md', 'desktop/src-tauri/README.en.md', 'desktop/src-tauri/icons/icon.png', 'desktop/src-tauri/updater.pub']:
            self.assertTrue((support/path).is_file(), path)
        self.assertTrue((support/'docs/UPDATES.md').is_file())
        translated_docs = ['ARCHITECTURE', 'PARALLELS_SMOKE', 'PRIOR_ART_AUDIT',
                           'SHADER_SPEC', 'UPDATES', 'WINDOWS_VALIDATION']
        for name in translated_docs:
            for language in ['', '.en']:
                path = Path('docs') / f'{name}{language}.md'
                self.assertEqual((support/path).read_bytes(), (self.root/path).read_bytes())
        self.assertTrue((support/'licenses/rust/manifest.json').is_file())
        self.assertFalse((support/'docs/CURRENT_STATE.md').exists())
        self.assertFalse((support/'docs/local-session.md').exists())
        self.assertFalse((support/'BUILD-STATUS.json').exists())
        self.assertFalse((support/'engine').exists())
        result = self.run_package()
        with zipfile.ZipFile(result['archive']) as archive:
            names = archive.namelist()
            for name in translated_docs:
                path = f'docs/{name}.en.md'
                self.assertEqual(archive.read(f'{Path(result["archive"]).stem}/{path}'),
                                 (self.root/path).read_bytes())
            self.assertFalse(any('CURRENT_STATE' in name or 'local-session' in name for name in names))
            self.assertFalse(any(private in archive.read(name) for name in names))
        # Fresh public checkouts have no checkpoint, and still package identically.
        (self.root/'docs/CURRENT_STATE.md').unlink()
        self.assertEqual(self.run_package()['sha256'], result['sha256'])

    def test_default_channel_is_off_and_invalid_metadata_cannot_replace_package(self):
        result = self.run_package()
        archive = Path(result['archive'])
        original = archive.read_bytes()
        with zipfile.ZipFile(archive) as zipped:
            status = json.loads(zipped.read(archive.stem + '/BUILD-STATUS.json'))
            self.assertEqual(status['update_channel'], 'unconfigured')
        self.args.update_channel = 'partial'
        with self.assertRaisesRegex(ValueError, 'Update channel'):
            self.run_package()
        self.assertEqual(archive.read_bytes(), original)

    @unittest.skipUnless(os.name == 'posix', 'POSIX builder guard')
    def test_partial_channel_rejected_before_any_build_without_printing_values(self):
        script = Path(__file__).with_name('build-desktop.sh').resolve()
        for supplied in ['LUMATAPE_UPDATE_ENDPOINT', 'LUMATAPE_UPDATE_PUBLIC_KEY']:
            with self.subTest(supplied=supplied):
                env = dict(os.environ)
                env.pop('LUMATAPE_UPDATE_ENDPOINT', None)
                env.pop('LUMATAPE_UPDATE_PUBLIC_KEY', None)
                env[supplied] = 'PRIVATE_TEST_VALUE_MUST_NOT_BE_PRINTED'
                # An invalid input makes an accidental continuation fail, but the
                # expected error must come from the update guard before any build.
                result = subprocess.run(['sh', str(script), str(self.root/'absent-native')],
                                        env=env, text=True, capture_output=True, timeout=10)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('requires both', result.stderr)
                self.assertNotIn(env[supplied], result.stdout + result.stderr)
                self.assertEqual(result.stdout, '')

    def test_gpu_only_native_input_cannot_produce_or_replace_package(self):
        self.run_package()
        archive = self.root/'dist/lumatape-0.2.0-windows-x64.zip'
        previous = archive.read_bytes()
        self.capture_pe(package_desktop.CAPTURE_EXPORTS - {'vhs_capture_open_ex', 'vhs_capture_get_stats'})
        with self.assertRaisesRegex(ValueError, 'CPU compatibility ABI.*vhs_capture_get_stats.*vhs_capture_open_ex'):
            self.run_package()
        self.assertEqual(previous, archive.read_bytes())
        self.assertFalse(list(archive.parent.glob('.desktop-*')))

    def test_malformed_export_table_is_rejected_before_staging(self):
        path = self.capture_pe(package_desktop.CAPTURE_EXPORTS)
        data = bytearray(path.read_bytes())
        struct.pack_into('<I', data, 512+32, 0x7fffffff)
        path.write_bytes(data)
        with self.assertRaisesRegex(ValueError, 'RVA'):
            self.run_package()
        self.assertFalse((self.root/'dist').exists())

    def test_exact_semver_archive_and_checksum_paths_are_preserved(self):
        result = self.run_package()
        folder = self.root/'dist/lumatape-0.2.0-windows-x64'
        archive = folder.with_name(folder.name + '.zip')
        checksum = folder.with_name(folder.name + '.zip.sha256')
        self.assertEqual(Path(result['archive']), archive)
        self.assertTrue(folder.is_dir())
        self.assertEqual(checksum.read_text(), f"{hashlib.sha256(archive.read_bytes()).hexdigest()}  lumatape-0.2.0-windows-x64.zip\n")
        self.assertFalse((folder.parent/'lumatape-0.2.zip').exists())
        self.assertFalse((folder.parent/'lumatape-0.2.zip.sha256').exists())
        with zipfile.ZipFile(archive) as zipped:
            self.assertIsNone(zipped.testzip())
            self.assertTrue(all(name.startswith(folder.name + '/') for name in zipped.namelist()))
            self.assertTrue(all(info.create_system == 3 for info in zipped.infolist()))
            self.assertNotIn(b'\r\n', zipped.read(folder.name + '/SOURCE-FILES.json'))
        first_bytes = archive.read_bytes()
        (folder/'stale.txt').write_text('must not carry over')
        self.run_package()
        self.assertEqual(archive.read_bytes(), first_bytes)
        self.assertFalse((folder/'stale.txt').exists())

    def test_rust_notices_cannot_be_missing_stale_or_tampered(self):
        text = self.root/'input/licenses/rust/sample-1.0/LICENSE'
        text.write_text('tampered')
        with self.assertRaisesRegex(ValueError, 'checksum'):
            self.run_package()
        self.assertEqual(list((self.root/'dist').iterdir()), [])
        (self.root/'input/licenses/rust/manifest.json').unlink()
        with self.assertRaisesRegex(ValueError, 'Missing Rust'):
            self.run_package()

    def test_sync_conflict_licenses_do_not_change_archive(self):
        initial = self.run_package()
        archive = Path(initial['archive'])
        first_bytes = archive.read_bytes()
        # Reproduce the observed iCloud LICENSE 2 duplicates after a build.
        for name in ['rust/sample-1.0/LICENSE 2', 'rust/sample-1.0/LICENSE 2.txt',
                     'rust/unrelated-9.9/LICENSE', 'rust/manifest 2.json',
                     'rust/THIRD-PARTY-NOTICES 2.txt', 'native/LICENSE',
                     'native/GLFW-LICENSE 2.md', 'README 2.txt',
                     'npm/packages/react/LICENSE 2', 'npm/manifest 2.json']:
            self.write('input/licenses/' + name, b'unreferenced sync conflict\n')
        self.write('input/licenses/rust/THIRD-PARTY-NOTICES.txt', b'stale index\n')
        repeated = self.run_package()
        self.assertEqual(initial['sha256'], repeated['sha256'])
        self.assertEqual(first_bytes, archive.read_bytes())
        with zipfile.ZipFile(archive) as zipped:
            prefix = 'lumatape-0.2.0-windows-x64/licenses/'
            actual = {name[len(prefix):] for name in zipped.namelist() if name.startswith(prefix)}
            expected = {'native/' + name for name in package_desktop.NATIVE_NOTICES}
            expected.update({'rust/manifest.json', 'rust/THIRD-PARTY-NOTICES.txt', 'rust/sample-1.0/LICENSE'})
            expected.update({'npm/manifest.json', 'npm/THIRD-PARTY-NOTICES.txt', 'npm/packages/react/LICENSE', 'npm/vendored/shadcn/LICENSE'})
            self.assertEqual(actual, expected)

    def test_npm_notices_require_current_lock_and_untampered_full_text(self):
        (self.root/'desktop/package-lock.json').write_text('changed lock')
        with self.assertRaisesRegex(ValueError, 'current lock/provenance'):
            self.run_package()
        (self.root/'desktop/package-lock.json').write_bytes(b'test notice\n')
        (self.root/'input/licenses/npm/packages/react/LICENSE').write_text('tampered')
        with self.assertRaisesRegex(ValueError, 'npm license text checksum'):
            self.run_package()
        (self.root/'input/licenses/npm/manifest.json').unlink()
        with self.assertRaisesRegex(ValueError, 'Missing npm'):
            self.run_package()


if __name__ == '__main__':
    unittest.main()
