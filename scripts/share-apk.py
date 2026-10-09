"""Serve only the APK on one local interface for two hours."""
import functools
import http.server
import pathlib
import secrets
import threading
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / '.tools' / 'qr-python'))
import qrcode

HOST = sys.argv[1]
PORT = 8765
PATH = '/' + secrets.token_urlsafe(18) + '/Blizko-android.apk'
APK = ROOT / 'dist' / 'Blizko-android.apk'
URL = f'http://{HOST}:{PORT}{PATH}'

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != PATH:
            self.send_error(404)
            return
        self.send_response(200)
        self.send_header('Content-Type', 'application/vnd.android.package-archive')
        self.send_header('Content-Disposition', 'attachment; filename="Blizko-android.apk"')
        self.send_header('Content-Length', str(APK.stat().st_size))
        self.send_header('Cache-Control', 'no-store')
        self.end_headers()
        try:
            with APK.open('rb') as source:
                while chunk := source.read(1024 * 1024):
                    self.wfile.write(chunk)
        except (BrokenPipeError, ConnectionResetError):
            pass

    def log_message(self, *args):
        pass

server = http.server.ThreadingHTTPServer((HOST, PORT), Handler)
qr = qrcode.QRCode(error_correction=qrcode.constants.ERROR_CORRECT_M, box_size=12, border=4)
qr.add_data(URL)
qr.make(fit=True)
qr.make_image(fill_color='black', back_color='white').save(ROOT / 'dist' / 'Blizko-download-qr.png')
(ROOT / '.tools' / 'apk-share-url.txt').write_text(URL, encoding='utf-8')
timer = threading.Timer(7200, server.shutdown)
timer.daemon = True
timer.start()
try:
    server.serve_forever()
finally:
    server.server_close()
