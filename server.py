import json
import tempfile
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse

from missing import HERE, analyze

PAGE = HERE / "web" / "index.html"
UPLOADS = Path(tempfile.mkdtemp(prefix="raas-uploads-"))


class Handler(BaseHTTPRequestHandler):
    def reply(self, status, body, content_type="application/json"):
        data = body if isinstance(body, bytes) else json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path == "/":
            self.reply(200, PAGE.read_bytes(), "text/html; charset=utf-8")
        elif self.path == "/api/defaults":
            self.reply(200, {key: str(HERE / name) for key, name in (("original", "game"), ("altered", "game-altered"), ("ghidra", "ghidra"))})
        else:
            self.reply(404, {"error": "not found"})

    def do_POST(self):
        url = urlparse(self.path)
        body = self.rfile.read(int(self.headers["Content-Length"]))
        if url.path == "/api/upload":
            name = Path(parse_qs(url.query)["name"][0]).name
            target = Path(tempfile.mkdtemp(dir=UPLOADS)) / name
            target.write_bytes(body)
            return self.reply(200, {"path": str(target)})
        if self.path != "/api/analyze":
            return self.reply(404, {"error": "not found"})
        request = json.loads(body)
        paths = {key: Path(request[key]).expanduser().resolve() for key in ("original", "altered", "ghidra")}
        try:
            self.reply(200, analyze(paths["original"], paths["altered"], str(paths["ghidra"])))
        except Exception as error:
            self.reply(500, {"error": str(error)})


if __name__ == "__main__":
    print("RAAS-1 web console on http://127.0.0.1:8000")
    ThreadingHTTPServer(("127.0.0.1", 8000), Handler).serve_forever()
