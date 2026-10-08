import hashlib
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location('npm_licenses', Path(__file__).with_name('npm-licenses.py'))
npm_licenses = importlib.util.module_from_spec(spec)
spec.loader.exec_module(npm_licenses)


class NpmLicenseTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.project = self.root / 'desktop'
        self.project.mkdir()
        self.lock = {'lockfileVersion': 3, 'packages': {'': {'dependencies': {'react': '1.2.3'}, 'devDependencies': {'tailwindcss': '2.0.0', 'vite': '3.0.0'}}}}
        self.add_package('react', '1.2.3')
        self.add_package('scheduler', '1.0.0')
        self.add_package('tailwindcss', '2.0.0', dev=True)
        self.add_package('vite', '3.0.0', dev=True)
        self.vendor = self.root / 'third_party/frontend-provenance.json'
        self.vendor.parent.mkdir()
        data = b'MIT full shadcn license\n'
        (self.vendor.parent / 'shadcn-LICENSE.txt').write_bytes(data)
        self.vendor.write_text(json.dumps({'packages': [{'name': 'shadcn/ui', 'source': 'https://github.com/shadcn-ui/ui', 'revision': 'pinned-source-commit', 'license': 'MIT', 'texts': [{'path': 'shadcn-LICENSE.txt', 'sha256': hashlib.sha256(data).hexdigest()}]}]}))

    def add_package(self, name, version, dev=False):
        entry = {'version': version, 'dev': dev, 'resolved': f'https://registry.npmjs.org/{name}/-/{name}-{version}.tgz', 'integrity': 'sha512-test'}
        self.lock['packages']['node_modules/' + name] = entry
        root = self.project / 'node_modules' / name
        root.mkdir(parents=True)
        (root/'package.json').write_text(json.dumps({'name': name, 'version': version, 'license': 'MIT'}))
        (root/'LICENSE').write_text('Full license of ' + name)
        (root/'LICENSE 2.txt').write_text('Conflict copy must not ship')

    def collect(self):
        (self.project/'package-lock.json').write_text(json.dumps(self.lock))
        return npm_licenses.collect(self.project, self.root/'out', self.vendor)

    def test_runtime_graph_css_vendor_and_no_build_tools_or_conflict_copies(self):
        report = self.collect()
        self.assertEqual({p['name'] for p in report['packages']}, {'react', 'scheduler', 'tailwindcss'})
        self.assertEqual(report['package_count'], 3)
        self.assertEqual(len(report['vendored']), 1)
        for package in report['packages'] + report['vendored']:
            self.assertEqual(len(package['texts']), 1)
            for text in package['texts']:
                self.assertEqual(hashlib.sha256((self.root/'out'/text['path']).read_bytes()).hexdigest(), text['sha256'])
        self.assertNotIn(str(self.root), json.dumps(report))
        self.assertFalse(any('LICENSE 2' in str(p) for p in (self.root/'out').rglob('*')))

    def test_version_or_missing_full_text_fails_without_partial_output(self):
        pkg = self.project/'node_modules/react/package.json'
        pkg.write_text(json.dumps({'name': 'react', 'version': 'other'}))
        with self.assertRaisesRegex(ValueError, 'does not match lock'):
            self.collect()
        self.assertFalse((self.root/'out').exists())
        pkg.write_text(json.dumps({'name': 'react', 'version': '1.2.3'}))
        (pkg.parent/'LICENSE').unlink()
        with self.assertRaisesRegex(ValueError, 'No full npm license'):
            self.collect()
        self.assertFalse((self.root/'out').exists())

    def test_vendor_checksum_and_package_boundary(self):
        (self.vendor.parent/'shadcn-LICENSE.txt').write_text('tampered')
        with self.assertRaisesRegex(ValueError, 'checksum'):
            self.collect()
        self.lock['packages']['../escaped'] = self.lock['packages'].pop('node_modules/react')
        with self.assertRaisesRegex(ValueError, 'Unsafe npm license path'):
            self.collect()

    def test_missing_published_license_supplement_requires_locked_integrity(self):
        (self.project/'node_modules/react/LICENSE').unlink()
        override = self.root/'overrides/react-1.2.3'
        override.mkdir(parents=True)
        data = b'Full official upstream MIT text\n'
        (override/'LICENSE').write_bytes(data)
        provenance = {'name': 'react', 'version': '1.2.3', 'integrity': 'sha512-test',
                      'source_commit': 'pinned-commit', 'texts': [{'path': 'LICENSE', 'sha256': hashlib.sha256(data).hexdigest()}]}
        (override/'provenance.json').write_text(json.dumps(provenance))
        (self.project/'package-lock.json').write_text(json.dumps(self.lock))
        report = npm_licenses.collect(self.project, self.root/'out', self.vendor, self.root/'overrides')
        react = next(p for p in report['packages'] if p['name'] == 'react')
        self.assertEqual(react['supplemental_provenance']['source_commit'], 'pinned-commit')
        provenance['integrity'] = 'sha512-other'
        (override/'provenance.json').write_text(json.dumps(provenance))
        with self.assertRaisesRegex(ValueError, 'does not match locked'):
            npm_licenses.collect(self.project, self.root/'other', self.vendor, self.root/'overrides')


if __name__ == '__main__':
    unittest.main()
