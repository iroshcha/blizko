"""GitHub Actions publication of a locally signed APK; no signing keys leave the PC."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import urllib.request
import urllib.parse

root = Path(__file__).resolve().parents[1]
metadata = json.loads((root / 'distribution' / 'android-release.json').read_text(encoding='utf-8-sig'))
version = metadata['versionName']
code = metadata['versionCode']
sha = metadata['sha256']
size = metadata['size']
source = metadata['sourceUrl']
assert re.fullmatch(r'\d+\.\d+\.\d+', version) and isinstance(code, int) and code > 0
assert re.fullmatch(r'[0-9a-f]{64}', sha) and isinstance(size, int) and 0 < size <= 200 * 1024 * 1024
assert source.startswith('https://') and '@' not in urllib.parse.urlparse(source).netloc
repo = os.environ['GITHUB_REPOSITORY']
tag = 'v' + version
out = root / 'dist'
out.mkdir(exist_ok=True)
apk = out / 'Blizko-android.apk'
digest = hashlib.sha256()
received = 0
with urllib.request.urlopen(source, timeout=60) as response, apk.open('wb') as target:
    assert response.url.startswith('https://'), 'Insecure download redirect'
    while chunk := response.read(1024 * 1024):
        received += len(chunk)
        assert received <= size, 'Unexpected APK size'
        digest.update(chunk)
        target.write(chunk)
assert received == size and digest.hexdigest() == sha, 'APK integrity mismatch'
feed = {
    'versionCode': code, 'versionName': version, 'sha256': sha, 'size': size,
    'url': f'https://github.com/{repo}/releases/download/{tag}/Blizko-android.apk'
}
(out / 'android-update.json').write_text(json.dumps(feed, indent=2) + '\n', encoding='utf-8')
notes = out / 'release-notes.txt'
notes.write_text('Контакты добавляются через QR камерой или из изображения. '
                 'Android поддерживает проверку и установку обновлений из приложения. '
                 'Устанавливайте поверх предыдущей версии, чтобы сохранить историю.\n', encoding='utf-8')
# Fail rather than replace an existing release or published binary.
subprocess.run(['gh', 'release', 'create', tag, str(apk), str(out / 'android-update.json'),
                '--repo', repo, '--title', 'Близко ' + version, '--notes-file', str(notes),
                '--target', os.environ['GITHUB_SHA']], check=True)
