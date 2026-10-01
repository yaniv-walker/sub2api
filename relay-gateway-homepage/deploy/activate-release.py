"""Manual release activation tool. No SSH, service reload or data deletion.

Run on the server only during an approved deployment:
  python3 activate-release.py --release r2-12345678
Default mode verifies only. Add --activate to atomically switch current.
"""
import argparse
import hashlib
import json
import os
import re
from pathlib import Path
import uuid


def verify(base, release_id):
    if not re.fullmatch(r'r[1-9][0-9]*-[0-9a-f]{8}', release_id):
        raise ValueError('Invalid release ID')
    releases = (base / 'releases').resolve()
    target = releases / release_id
    if target.is_symlink() or not target.is_dir():
        raise ValueError('Release must be a real directory')
    manifest = json.loads((target / 'release-manifest.json').read_text('utf-8'))
    if manifest['id'] != release_id:
        raise ValueError('Manifest ID mismatch')
    expected = set(manifest['files']) | {'release-manifest.json'}
    actual = set()
    for item in target.rglob('*'):
        if item.is_symlink():
            raise ValueError('Symlinks inside a release are forbidden')
        if item.is_file():
            actual.add(item.relative_to(target).as_posix())
    if actual != expected:
        raise ValueError('Unexpected or missing files')
    for name, digest in manifest['files'].items():
        allowed = name == 'index.html' or name == 'homepage/config.json' or re.fullmatch(r'homepage-assets/[A-Za-z0-9_/-]+\.(mjs|css|svg|png|webp|jpe?g)', name)
        if not allowed or '..' in name:
            raise ValueError('File outside public allowlist: ' + name)
        if hashlib.sha256((target / name).read_bytes()).hexdigest() != digest:
            raise ValueError('Hash mismatch: ' + name)
    config = json.loads((target / 'homepage/config.json').read_text('utf-8'))
    if config['schema_version'] != 1 or config['revision'] != manifest['revision']:
        raise ValueError('Config revision mismatch')
    return target


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base', default='/srv/sub2api-homepage')
    parser.add_argument('--release', required=True)
    parser.add_argument('--activate', action='store_true')
    args = parser.parse_args()
    base = Path(args.base).resolve()
    target = verify(base, args.release)
    print('Verified:', target)
    if not args.activate:
        print('Verification only; current unchanged.')
        return
    current = base / 'current'
    if current.exists() and not current.is_symlink():
        raise ValueError('Refusing to replace a non-symlink current directory')
    if current.is_symlink():
        old = current.resolve()
        if old.parent != (base / 'releases').resolve():
            raise ValueError('Existing current escapes releases directory')
        print('Previous release:', old.name)
    temporary = base / ('.current-' + uuid.uuid4().hex)
    try:
        temporary.symlink_to(target, target_is_directory=True)
        os.replace(temporary, current)
    finally:
        if temporary.is_symlink():
            temporary.unlink()
    print('Activated:', args.release)


if __name__ == '__main__':
    main()
