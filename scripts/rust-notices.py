"""Collect the exact Cargo resolution's license notices into both mobile bundles."""
import json
from pathlib import Path
import subprocess

root = Path(__file__).resolve().parent.parent
metadata = json.loads(subprocess.check_output(['cargo', 'metadata', '--format-version', '1', '--locked'], cwd=root/'core/iroh'))
parts = ['Blizko: Rust / iroh dependency notices. Includes build-only dependencies.\n']
for package in sorted(metadata['packages'], key=lambda p: (p['name'], p['version'])):
    parts.append(f"\n--- {package['name']} {package['version']} / {package.get('license') or 'see license file'} ---\n")
    directory = Path(package['manifest_path']).parent
    files = [p for p in directory.iterdir() if p.is_file() and p.name.upper().startswith(('LICENSE', 'COPYING', 'NOTICE', 'UNLICENSE'))]
    if package.get('license_file'):
        files.append(directory/package['license_file'])
    for path in sorted(set(files)):
        parts.append(path.read_text(encoding='utf-8', errors='replace'))
for target in ['app/src/main/assets/IROH_NOTICES.txt','ios/Sources/Resources/IROH_NOTICES.txt']:
    (root/target).write_text('\n'.join(parts), encoding='utf-8')
