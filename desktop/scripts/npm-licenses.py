#!/usr/bin/env python3
"""Collect locked frontend runtime/CSS and vendored shadcn notices; no network."""
import argparse
import hashlib
import json
import re
from pathlib import Path, PurePosixPath

CSS_OUTPUT_PACKAGES = ('tailwindcss', 'tw-animate-css')
MAX_TEXT = 2 * 1024 * 1024


def digest(data):
    return hashlib.sha256(data).hexdigest()


def relative_path(value):
    path = PurePosixPath(value)
    if path.is_absolute() or '..' in path.parts or '\\' in value or ':' in value or not path.parts:
        raise ValueError('Unsafe npm license path')
    return path


def full_texts(root):
    # Full upstream notices, including nested dual licenses; never traverse
    # another package, tests, or arbitrary source. Conflict copies are ignored.
    found = []
    def visit(directory, depth):
        for path in sorted(directory.iterdir()):
            if path.is_symlink():
                continue
            if path.is_dir():
                if depth < 3 and path.name.lower() in ('license', 'licenses', 'licence', 'licences'):
                    visit(path, depth + 1)
                continue
            if not re.match(r'^(licen[cs]e|copying|notice)(?:$|[._-])', path.name, re.I):
                continue
            if re.search(r' \d+(?:\.[^.]*)?$', path.name):
                continue
            data = path.read_bytes()
            if not data or len(data) > MAX_TEXT or b'\0' in data:
                raise ValueError(f'Unsupported npm license text: {path.name}')
            found.append((path.relative_to(root).as_posix(), data))
    visit(root, 0)
    return found


def collect(project, output, vendor_manifest, overrides=None):
    if overrides is None:
        overrides = Path(__file__).with_name('npm-license-overrides')
    lock_path = project / 'package-lock.json'
    lock_bytes = lock_path.read_bytes()
    lock = json.loads(lock_bytes)
    if lock.get('lockfileVersion') not in (2, 3) or 'packages' not in lock:
        raise ValueError('npm package-lock v2/v3 is required')
    if output.exists() and any(output.iterdir()):
        raise ValueError('npm license output must be absent or empty')
    packages = lock['packages']
    roots = packages.get('', {})
    declared = set(roots.get('dependencies', {})) | set(roots.get('devDependencies', {}))
    css_paths = {'node_modules/' + name for name in CSS_OUTPUT_PACKAGES if name in declared}
    selected = [key for key, entry in packages.items() if key and (not entry.get('dev', False) or key in css_paths)]
    records, files = [], {}
    for key in sorted(selected):
        entry = packages[key]
        relative = relative_path(key)
        if relative.parts[0] != 'node_modules' or entry.get('link'):
            raise ValueError('Frontend release dependencies must be locked npm packages')
        package_root = (project / relative).resolve()
        if not package_root.is_relative_to((project / 'node_modules').resolve()):
            raise ValueError('npm package resolves outside node_modules')
        installed = json.loads((package_root / 'package.json').read_text(encoding='utf-8'))
        if installed.get('version') != entry.get('version'):
            raise ValueError(f'Installed npm version does not match lock: {key}')
        resolved, integrity = entry.get('resolved', ''), entry.get('integrity', '')
        if not resolved.startswith('https://registry.npmjs.org/') or not integrity.startswith(('sha512-', 'sha256-')):
            raise ValueError(f'Expected integrity-pinned npm registry package: {key}')
        identifier = digest(key.encode())[:16]
        texts = full_texts(package_root)
        supplement = None
        if not texts:
            override = overrides / f'{installed.get("name", "").replace("/", "__")}-{entry["version"]}'
            if (override / 'provenance.json').is_file():
                supplement = json.loads((override / 'provenance.json').read_text(encoding='utf-8'))
                if (supplement.get('name') != installed.get('name') or supplement.get('version') != entry['version']
                        or supplement.get('integrity') != integrity or not supplement.get('source_commit')):
                    raise ValueError('npm license supplement does not match locked package')
                for text in supplement['texts']:
                    path = (override / relative_path(text['path'])).resolve()
                    if not path.is_relative_to(override.resolve()):
                        raise ValueError('npm license supplement path escaped')
                    data = path.read_bytes()
                    if digest(data) != text['sha256']:
                        raise ValueError('npm license supplement checksum mismatch')
                    texts.append((text['path'], data))
        if not texts:
            raise ValueError(f'No full npm license text: {key}')
        record = dict(name=installed.get('name'), version=entry['version'], lock_path=key,
                      resolved=resolved, integrity=integrity, license=installed.get('license'),
                      scope='generated-css' if entry.get('dev', False) else 'runtime-dependency-graph', texts=[])
        if supplement:
            record['supplemental_provenance'] = supplement
        for name, data in texts:
            destination = 'packages/' + identifier + '/' + name
            files[destination] = data
            record['texts'].append(dict(path=destination, sha256=digest(data)))
        records.append(record)
    if not records:
        raise ValueError('Frontend runtime dependency graph is empty')
    vendor_bytes = vendor_manifest.read_bytes()
    vendor = json.loads(vendor_bytes)
    vendors = []
    for record in vendor['packages']:
        if not record.get('source') or not record.get('revision') or not record.get('texts'):
            raise ValueError('Vendored UI license requires pinned provenance and full text')
        copy = dict(record, texts=[])
        for text in record['texts']:
            path = (vendor_manifest.parent / relative_path(text['path'])).resolve()
            if not path.is_relative_to(vendor_manifest.parent.resolve()):
                raise ValueError('Vendored license outside provenance directory')
            data = path.read_bytes()
            if digest(data) != text['sha256']:
                raise ValueError('Vendored UI license checksum mismatch')
            dest = 'vendored/' + digest(record['name'].encode())[:16] + '/' + path.name
            files[dest] = data
            copy['texts'].append(dict(path=dest, sha256=digest(data)))
        vendors.append(copy)
    if not any(p['name'] == 'shadcn/ui' for p in vendors):
        raise ValueError('Missing shadcn/ui full license and provenance')
    manifest = dict(schema_version=1, package_lock_sha256=digest(lock_bytes),
                    vendor_manifest_sha256=digest(vendor_bytes), package_count=len(records),
                    packages=records, vendored=vendors, missing_texts=[],
                    scope='Locked non-dev dependency graph plus explicit generated CSS and vendored UI; build tools excluded')
    files['manifest.json'] = (json.dumps(manifest, indent=2, ensure_ascii=False) + '\n').encode()
    index = ['LumaTape frontend dependency notices', '', manifest['scope'], '']
    for package in records + vendors:
        index.append(f'{package["name"]} {package.get("version", package.get("revision", ""))}: {package.get("license", "See full text")}')
        index.extend('  ' + t['path'] for t in package['texts'])
    files['THIRD-PARTY-NOTICES.txt'] = ('\n'.join(index) + '\n').encode()
    for name, data in sorted(files.items()):
        path = output / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--project', type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--vendor-manifest', type=Path, default=Path(__file__).resolve().parents[2] / 'third_party/frontend-provenance.json')
    args = parser.parse_args()
    report = collect(args.project, args.output, args.vendor_manifest)
    print(json.dumps(dict(packages=report['package_count'], vendored=len(report['vendored']), missing_texts=report['missing_texts'])))


if __name__ == '__main__':
    main()
