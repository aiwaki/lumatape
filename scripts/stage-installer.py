#!/usr/bin/env python3
"""Stage the same verified notices and public docs as the portable package."""
import argparse
import importlib.util
from pathlib import Path
import shutil
import tempfile

ROOT = Path(__file__).resolve().parent.parent
SPEC = importlib.util.spec_from_file_location('package_desktop', Path(__file__).with_name('package-desktop.py'))
PACKAGE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PACKAGE)


def stage(licenses):
    output = ROOT / 'desktop/src-tauri/resources/support'
    output.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.support-', dir=output.parent) as temporary:
        ready = Path(temporary) / 'support'
        ready.mkdir()
        PACKAGE.copy_support_files(ready, licenses)
        # Only this generated staging directory is replaced, after validation.
        if output.is_symlink():
            raise ValueError('Installer staging must not be a symlink')
        if output.exists():
            shutil.rmtree(output)
        ready.rename(output)
    print(f'Installer support: {sum(p.is_file() for p in output.rglob("*"))} verified files')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--licenses', required=True, type=Path)
    stage(parser.parse_args().licenses.resolve())
