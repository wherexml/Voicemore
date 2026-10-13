#!/usr/bin/env python3
"""Local-only E2E update feed. mode: corrupt, valid, current, unavailable, malformed.
With regular Voicemore quit, launch the prepared fixture; check/install from Home.
Corrupt must fail without changing the installed binary. Set mode to valid and
retry: the client must install the signed real build and restart as that version.
The relaunched production app uses the real GitHub feed, not this fixture.
"""
import http.server
import json
from pathlib import Path
import time

ROOT = Path(__file__).resolve().parent.parent
FIXTURE = ROOT / 'artifacts/updater-e2e'
MANIFEST = ROOT / 'build/darwin-arm64/update-darwin-arm64.json'

class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        mode = (FIXTURE / 'mode').read_text().strip()
        if mode == 'unavailable':
            self.send_error(503)
            return
        manifest = json.loads(MANIFEST.read_text())
        if self.path == '/update-darwin-arm64.json':
            if mode == 'current':
                manifest['version'] = '0.2.9'
            manifest['url'] = 'http://127.0.0.1:48139/archive.tar.gz'
            body = b'not json' if mode == 'malformed' else json.dumps(manifest).encode()
            self.send_response(200)
            self.send_header('Content-Length', str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        elif self.path == '/archive.tar.gz':
            archive = MANIFEST.parent / f"voicemore-{manifest['version']}-darwin-arm64.tar.gz"
            self.send_response(200)
            self.send_header('Content-Length', str(archive.stat().st_size))
            self.end_headers()
            try:
                with archive.open('rb') as stream:
                    first = True
                    while data := stream.read(1024 * 1024):
                        if first and mode == 'corrupt':
                            data = bytes([data[0] ^ 1]) + data[1:]
                        first = False
                        self.wfile.write(data)
                        time.sleep(0.3)
            except (BrokenPipeError, ConnectionResetError):
                pass  # Expected when the user cancels a transfer.
        else:
            self.send_error(404)

if __name__ == '__main__':
    print('Local updater E2E feed: http://127.0.0.1:48139', flush=True)
    http.server.ThreadingHTTPServer(('127.0.0.1', 48139), Handler).serve_forever()
