#!/usr/bin/env python3
"""Allowlisted, deterministic portable Tauri package. Does not qualify runtime."""
import argparse
import hashlib
import json
import shutil
import struct
import tempfile
import zipfile
from pathlib import Path, PurePosixPath

ROOT = Path(__file__).resolve().parent.parent
ENGINE = ('lumatape-engine.exe', 'lumatape-watchdog.exe', 'lumatape-testcard.exe', 'glfw3.dll', 'lumatape_capture.dll')
CAPTURE_EXPORTS = frozenset('vhs_capture_' + name for name in (
    'abi_version', 'open', 'open_ex', 'get_stats', 'acquire', 'release', 'close'))
NATIVE_NOTICES = ('GLFW-LICENSE.md', 'Go-LICENSE.txt', 'cppwinrt-LICENSE.txt', 'MSVC-STL-LICENSE.txt')


def check_capture_abi(path):
    """Read PE exports without loading Windows code; reject stale GPU-only DLLs.

    PE32+ layout: https://learn.microsoft.com/windows/win32/debug/pe-format
    This packaging check cannot replace executing the native ABI smoke test.
    """
    data = path.read_bytes()

    def number(fmt, offset):
        if offset < 0 or offset + struct.calcsize(fmt) > len(data):
            raise ValueError('Truncated capture PE export data')
        return struct.unpack_from(fmt, data, offset)[0]

    pe = number('<I', 0x3c)
    if data[:2] != b'MZ' or data[pe:pe+4] != b'PE\0\0' or number('<H', pe+4) != 0x8664:
        raise ValueError('Capture DLL must be Windows x64 PE')
    optional = pe + 24
    optional_size, section_count = number('<H', pe+20), number('<H', pe+6)
    if optional_size < 120 or number('<H', optional) != 0x20b or not 1 <= section_count <= 96:
        raise ValueError('Invalid capture PE32+ header')
    if number('<I', optional+108) < 1:
        raise ValueError('Capture DLL has no export directory')
    section_table = optional + optional_size
    headers_size = number('<I', optional+60)

    def file_offset(rva, size):
        if rva and rva < headers_size and rva + size <= min(headers_size, len(data)):
            return rva
        for index in range(section_count):
            section = section_table + index * 40
            address, raw_size, raw = (number('<I', section+x) for x in (12, 16, 20))
            delta = rva - address
            if rva and 0 <= delta and delta + size <= raw_size and raw + delta + size <= len(data):
                return raw + delta
        raise ValueError('Capture export RVA is outside file-backed sections')

    directory = file_offset(number('<I', optional+112), 40)
    function_count, name_count = number('<I', directory+20), number('<I', directory+24)
    if not 0 < name_count <= function_count <= 65536:
        raise ValueError('Invalid capture export count')
    functions = file_offset(number('<I', directory+28), function_count*4)
    names = file_offset(number('<I', directory+32), name_count*4)
    ordinals = file_offset(number('<I', directory+36), name_count*2)
    exports = set()
    for index in range(name_count):
        ordinal = number('<H', ordinals+index*2)
        if ordinal >= function_count or not number('<I', functions+ordinal*4):
            raise ValueError('Capture export has no valid function address')
        name_rva = number('<I', names+index*4)
        # ABI names are short ASCII; never scan beyond mapped file data.
        name = bytearray()
        for length in range(256):
            byte = data[file_offset(name_rva+length, 1)]
            if not byte:
                break
            name.append(byte)
        else:
            raise ValueError('Capture export name is not bounded')
        exports.add(name.decode('ascii', errors='replace'))
    missing = CAPTURE_EXPORTS - exports
    if missing:
        raise ValueError('Capture DLL lacks required CPU compatibility ABI exports: ' + ', '.join(sorted(missing)))
    return exports


def copy_pe(src, dst, gui=False):
    data = src.read_bytes()
    if data[:2] != b'MZ' or len(data) < 64:
        raise ValueError(f'Invalid PE: {src.name}')
    offset = struct.unpack_from('<I', data, 0x3c)[0]
    if data[offset:offset+4] != b'PE\0\0' or struct.unpack_from('<H', data, offset+4)[0] != 0x8664:
        raise ValueError(f'Expected Windows x64: {src.name}')
    if gui and struct.unpack_from('<H', data, offset+24+68)[0] != 2:
        raise ValueError(f'Expected Windows GUI subsystem: {src.name}')
    dst.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(src, dst)


def check_rust_licenses(directory):
    """Return only verified manifest-declared files, captured as checked bytes.

    Never enumerate/copy a license directory: sync conflict copies such as
    LICENSE 2 are not dependency inputs. Supplemental provenance is already
    embedded in manifest.json; its duplicate per-crate JSON is unnecessary.
    """
    rust = directory / 'rust'
    manifest_path = rust / 'manifest.json'
    if not manifest_path.is_file():
        raise ValueError('Missing Rust license manifest; run desktop/scripts/licenses.py')
    manifest_bytes = manifest_path.read_bytes()
    manifest = json.loads(manifest_bytes.decode('utf-8'))
    files = {Path('rust/manifest.json'): manifest_bytes}
    packages = manifest.get('packages', [])
    if not packages or manifest.get('package_count') != len(packages) or manifest.get('missing_texts') != []:
        raise ValueError('Rust dependency license collection is incomplete')
    lock_hash = hashlib.sha256((ROOT / 'desktop/src-tauri/Cargo.lock').read_bytes()).hexdigest()
    if manifest.get('cargo_lock_sha256') != lock_hash:
        raise ValueError('Rust license manifest does not match Cargo.lock')
    for package_record in packages:
        if not package_record.get('texts'):
            raise ValueError('Rust dependency has no full license text')
        for text in package_record['texts']:
            relative = PurePosixPath(text['path'])
            if relative.is_absolute() or '..' in relative.parts or '\\' in text['path'] or ':' in text['path'] or len(relative.parts) < 2:
                raise ValueError('Rust license text path is not a package-relative path')
            path = (rust / relative).resolve()
            if not path.is_relative_to(rust.resolve()) or not path.is_file():
                raise ValueError('Rust license text path is missing or outside its directory')
            data = path.read_bytes()
            if hashlib.sha256(data).hexdigest() != text['sha256']:
                raise ValueError('Rust license text checksum mismatch')
            files[Path('rust') / relative] = data
    # Reconstruct the small reader index from the exact same manifest. Do not
    # pick up a stale or conflict-renamed index from the input directory either.
    lines = ['LumaTape Rust dependency notices', '',
             'Full available license texts accompany this index.',
             'The SPDX declaration is not a replacement for the included license texts.', '']
    for package_record in packages:
        lines.append(f'{package_record.get("name", "unknown")} {package_record.get("version", "unknown")}: {package_record.get("license") or "undeclared"}')
        if package_record.get('source_archive'):
            lines.append('  Unmodified source: ' + package_record['source_archive'])
        lines.extend('  ' + text['path'] for text in package_record['texts'])
        lines.extend('  WARNING: ' + warning for warning in package_record.get('warnings', []))
    files[Path('rust/THIRD-PARTY-NOTICES.txt')] = ('\n'.join(lines) + '\n').encode('utf-8')
    return files


def check_npm_licenses(directory):
    npm = directory / 'npm'
    manifest_path = npm / 'manifest.json'
    if not manifest_path.is_file():
        raise ValueError('Missing npm license manifest; run desktop/scripts/npm-licenses.py')
    manifest_bytes = manifest_path.read_bytes()
    manifest = json.loads(manifest_bytes.decode('utf-8'))
    for key, source in (
        ('package_lock_sha256', ROOT / 'desktop/package-lock.json'),
        ('vendor_manifest_sha256', ROOT / 'third_party/frontend-provenance.json'),
    ):
        if manifest.get(key) != hashlib.sha256(source.read_bytes()).hexdigest():
            raise ValueError('npm license manifest does not match current lock/provenance')
    packages, vendored = manifest.get('packages', []), manifest.get('vendored', [])
    if not packages or manifest.get('package_count') != len(packages) or manifest.get('missing_texts') != []:
        raise ValueError('npm dependency license collection is incomplete')
    if not any(p.get('name') == 'shadcn/ui' for p in vendored):
        raise ValueError('Missing vendored shadcn/ui license provenance')
    files = {Path('npm/manifest.json'): manifest_bytes}
    lines = ['LumaTape frontend dependency notices', '']
    for record in packages + vendored:
        if not record.get('texts'):
            raise ValueError('Frontend dependency has no full license text')
        lines.append(f'{record.get("name", "unknown")} {record.get("version", record.get("revision", ""))}: {record.get("license", "See full text")}')
        for text in record['texts']:
            relative = PurePosixPath(text['path'])
            if relative.is_absolute() or '..' in relative.parts or '\\' in text['path'] or ':' in text['path'] or len(relative.parts) < 3:
                raise ValueError('Invalid npm license relative path')
            source = (npm / relative).resolve()
            if not source.is_relative_to(npm.resolve()) or not source.is_file():
                raise ValueError('npm license text is outside its directory or missing')
            data = source.read_bytes()
            if hashlib.sha256(data).hexdigest() != text['sha256']:
                raise ValueError('npm license text checksum mismatch')
            files[Path('npm') / relative] = data
            lines.append('  ' + text['path'])
    files[Path('npm/THIRD-PARTY-NOTICES.txt')] = ('\n'.join(lines) + '\n').encode('utf-8')
    return files


def copy_licenses(source, destination):
    files = check_rust_licenses(source)
    files.update(check_npm_licenses(source))
    for name in NATIVE_NOTICES:
        path = source / 'native' / name
        if not path.is_file():
            raise ValueError(f'Missing native notice: {name}')
        files[Path('native') / name] = path.read_bytes()
    for relative, data in sorted(files.items()):
        path = destination / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)


def copy_support_files(destination: Path, licenses: Path):
    """Copy only public documents, examples and verified dependency notices.

    Shared by portable packaging and installer resource staging. No runtime
    binaries, local checkpoint, build status or package manifests are copied.
    """
    copy_licenses(licenses, destination / 'licenses')
    for name in ('LICENSE', 'README.md', 'README.en.md', 'START-HERE.txt', 'START-HERE.en.txt', 'assets/lumatape.svg', 'assets/lumatape.ico', 'third_party/README.md', 'third_party/README.en.md', 'desktop/src-tauri/README.md', 'desktop/src-tauri/README.en.md', 'cmd/lumatape-testcard/README.md', 'cmd/lumatape-testcard/README.en.md', 'internal/control/README.md', 'internal/control/README.en.md', 'internal/display/README.md', 'internal/display/README.en.md', 'internal/pointer/README.md', 'internal/pointer/README.en.md', 'native/capture/README.md', 'native/capture/README.en.md', 'scripts/windows-control-smoke/README.md', 'scripts/windows-control-smoke/README.en.md', 'scripts/windows-ui-smoke/README.md', 'scripts/windows-ui-smoke/README.en.md', 'desktop/src-tauri/icons/icon.png', 'desktop/src-tauri/updater.pub'):
        dest = destination / name
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(ROOT / name, dest)
    for name in ('README.md', 'README.en.md', 'shaders/README.md', 'shaders/README.en.md', 'WINDOWS_VALIDATION.md', 'PARALLELS_SMOKE.md', 'ARCHITECTURE.md', 'PRIOR_ART_AUDIT.md', 'SHADER_SPEC.md', 'UPDATES.md'):
        target = destination / 'docs' / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(ROOT / 'docs' / name, target)
    for name in ('amber-crt.lumatape.glsl', 'cold-bleed.lumatape.glsl'):
        dest = destination / 'examples' / 'shaders' / name
        dest.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(ROOT / 'examples' / 'shaders' / name, dest)


def package(args):
    # This records an explicit build input; the packager cannot infer compile-time
    # option_env! values from an already-built EXE or the current shell environment.
    update_channel = getattr(args, 'update_channel', 'unconfigured')
    if update_channel not in ('unconfigured', 'configured'):
        raise ValueError('Update channel must be unconfigured or configured')
    # Refuse an obsolete install input before producing or replacing any ZIP.
    check_capture_abi(args.engine / 'lumatape_capture.dll')
    output = (args.output or ROOT / 'dist' / f'lumatape-{args.version}-windows-x64').resolve()
    # Never let a caller replace input/source or put output inside a source tree.
    inputs = [args.shell.resolve(), args.engine.resolve(), args.licenses.resolve()]
    if output == ROOT or output in ROOT.parents or any(output == p or output in p.parents or p in output.parents for p in inputs):
        raise ValueError('Output must not overlap source or inputs')
    if output.parent != ROOT / 'dist':
        raise ValueError('Output must be a direct child of repository dist/')
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.desktop-', dir=output.parent) as temporary:
        stage = Path(temporary) / output.name
        stage.mkdir()
        copy_pe(args.shell, stage / 'lumatape.exe', gui=True)
        for name in ENGINE:
            copy_pe(args.engine / name, stage / 'engine' / name, gui=name in ('lumatape-engine.exe','lumatape-testcard.exe'))
        copy_support_files(stage, args.licenses)
        status = dict(product='LumaTape', version=args.version, revision=args.revision, build_time=args.build_time,
                      qualification='UNQUALIFIED', backend='Windows SDR windowed/borderless',
                      update_channel=update_channel,
                      updater=('unconfigured; no automatic network access' if update_channel == 'unconfigured'
                               else 'configured; signed update checks enabled; in-place installation disabled for this portable ZIP'),
                      webview2='not required; native tray only',
                      interface='Tauri native menu; no WebView window',
                      physical_gpu='not verified', instructions='docs/WINDOWS_VALIDATION.md')
        (stage / 'BUILD-STATUS.json').write_text(json.dumps(status, ensure_ascii=False, indent=2)+'\n', encoding='utf-8', newline='\n')
        files = sorted(p for p in stage.rglob('*') if p.is_file())
        records = [dict(name=p.relative_to(stage).as_posix(), sha256=hashlib.sha256(p.read_bytes()).hexdigest(), size=p.stat().st_size) for p in files]
        (stage / 'SOURCE-FILES.json').write_text(json.dumps(records, indent=2)+'\n', encoding='utf-8', newline='\n')
        source_hash = hashlib.sha256((stage / 'SOURCE-FILES.json').read_bytes()).hexdigest()
        (stage / 'SHA256SUMS.txt').write_text(''.join(f"{r['sha256']}  {r['name']}\n" for r in records)+f'{source_hash}  SOURCE-FILES.json\n', encoding='utf-8', newline='\n')
        archive = output.with_name(output.name + '.zip')
        # The candidate stays inside the fresh temporary directory, so a stale
        # sibling .tmp file/symlink can never become a packaging write target.
        candidate = Path(temporary) / archive.name
        with zipfile.ZipFile(candidate, 'w', zipfile.ZIP_DEFLATED, compresslevel=9) as z:
            for p in sorted(stage.rglob('*')):
                if p.is_file():
                    info = zipfile.ZipInfo(f'{output.name}/{p.relative_to(stage).as_posix()}', (1980,1,1,0,0,0))
                    info.create_system = 3  # fixed UNIX attribute interpretation on every host
                    info.compress_type = zipfile.ZIP_DEFLATED
                    info.external_attr = 0o100644 << 16
                    z.writestr(info, p.read_bytes(), compresslevel=9)
        with zipfile.ZipFile(candidate) as z:
            assert z.testzip() is None
            for p in stage.rglob('*'):
                if p.is_file():
                    assert z.read(f'{output.name}/{p.relative_to(stage).as_posix()}') == p.read_bytes()
        if output.exists():
            shutil.rmtree(output)  # validated generated dist child only
        shutil.move(stage, output)
        candidate.replace(archive)
        digest = hashlib.sha256(archive.read_bytes()).hexdigest()
        archive.with_name(archive.name + '.sha256').write_text(f'{digest}  {archive.name}\n', encoding='utf-8', newline='\n')
        print(json.dumps(dict(archive=str(archive), sha256=digest, files=len(records)+2, bytes=archive.stat().st_size)))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--shell', type=Path, required=True)
    parser.add_argument('--engine', type=Path, required=True)
    parser.add_argument('--licenses', type=Path, required=True)
    parser.add_argument('--output', type=Path)
    parser.add_argument('--version', default='0.3.0')
    parser.add_argument('--revision', default='working-tree')
    parser.add_argument('--build-time', default='unknown')
    parser.add_argument('--update-channel', choices=('unconfigured', 'configured'), default='unconfigured',
                        help='Update channel compiled into the shell; portable ZIP never installs in place')
    package(parser.parse_args())
