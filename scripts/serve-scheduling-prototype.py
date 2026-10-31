#!/usr/bin/env python3
"""Throwaway, loopback-only scheduling prototype. No database or auth services."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit
import mimetypes
ROOT = Path(__file__).resolve().parents[1]
class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        path = urlsplit(self.path).path
        if path in ('/', '/prototype/volunteer-scheduling'):
            target = ROOT / 'docs/wayfinder/volunteer-scheduling/validation/prototype.html'
        elif path.startswith('/static/'):
            base = (ROOT / 'internal/ui/static').resolve()
            target = (base / path.removeprefix('/static/')).resolve()
            if not target.is_relative_to(base):
                self.send_error(404); return
        else:
            self.send_error(404); return
        if not target.is_file():
            self.send_error(404); return
        data = target.read_bytes()
        self.send_response(200)
        self.send_header('Content-Type', mimetypes.guess_type(target)[0] or 'application/octet-stream')
        self.send_header('Cache-Control', 'no-store')
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)
print('PROTOTYPE: http://127.0.0.1:8090/prototype/volunteer-scheduling?variant=timeline', flush=True)
ThreadingHTTPServer(('127.0.0.1', 8090), Handler).serve_forever()
