"""A minimal MCP server over Streamable HTTP: initialize, ping, tools/list, tools/call.

It answers every POST with one JSON body (no event stream) and refuses GET
with 405, as the transport allows a server that never speaks first. It is
meant for loopback: requests whose Host is not a loopback name are refused,
so a page in a browser cannot reach it by DNS rebinding.
"""

import json
import logging

log = logging.getLogger("mcp")

PROTOCOL_VERSIONS = ("2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05")
MAX_BODY = 1 << 20
LOOPBACK_HOSTS = ("127.0.0.1", "localhost", "[::1]")


class ToolError(Exception):
    """A refusal the model reads as the tool's result (isError true)."""


class Tools:
    def __init__(self, server_name, version):
        self.info = {"name": server_name, "version": version}
        self.definitions = []
        self.functions = {}

    def add(self, definition, function):
        self.definitions.append(definition)
        self.functions[definition["name"]] = function

    def handle(self, request):
        """The response to one JSON-RPC message, or None for a notification."""
        if not isinstance(request, dict) or request.get("jsonrpc") != "2.0":
            return error(None, -32600, "invalid request")
        if "id" not in request:
            return None
        rid, method = request["id"], request.get("method")
        params = request.get("params") or {}
        if method == "initialize":
            asked = params.get("protocolVersion")
            version = asked if asked in PROTOCOL_VERSIONS else PROTOCOL_VERSIONS[1]
            return result(rid, {"protocolVersion": version, "capabilities": {"tools": {"listChanged": False}}, "serverInfo": self.info})
        if method == "ping":
            return result(rid, {})
        if method == "tools/list":
            return result(rid, {"tools": self.definitions})
        if method == "tools/call":
            function = self.functions.get(params.get("name"))
            if function is None:
                return error(rid, -32602, f"unknown tool {params.get('name')!r}")
            arguments = params.get("arguments") or {}
            if not isinstance(arguments, dict):
                return error(rid, -32602, "arguments must be an object")
            try:
                out = function(arguments)
            except ToolError as exc:
                return result(rid, {"content": [{"type": "text", "text": str(exc)}], "isError": True})
            except Exception as exc:
                log.exception("tool %s", params.get("name"))
                return result(rid, {"content": [{"type": "text", "text": f"internal error: {exc}"}], "isError": True})
            return result(rid, {"content": [{"type": "text", "text": json.dumps(out)}], "isError": False})
        return error(rid, -32601, f"method not found: {method}")


def result(rid, value):
    return {"jsonrpc": "2.0", "id": rid, "result": value}


def error(rid, code, message):
    return {"jsonrpc": "2.0", "id": rid, "error": {"code": code, "message": message}}


def handler(tools, path="/mcp"):
    from http.server import BaseHTTPRequestHandler

    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, fmt, *args):
            pass

        def _send(self, status, body=b"", ctype="application/json", extra=None):
            self.send_response(status)
            if body:
                self.send_header("Content-Type", ctype)
            self.send_header("Content-Length", str(len(body)))
            for key, value in (extra or {}).items():
                self.send_header(key, value)
            self.end_headers()
            if body:
                self.wfile.write(body)

        def _host_ok(self):
            host = (self.headers.get("Host") or "").rsplit(":", 1)[0] if not (self.headers.get("Host") or "").startswith("[") else (self.headers.get("Host") or "").split("]")[0] + "]"
            return host in LOOPBACK_HOSTS

        def do_GET(self):
            self._send(405, b'{"error":"this server sends no event stream"}', extra={"Allow": "POST"})

        def do_DELETE(self):
            self._send(405, b"", extra={"Allow": "POST"})

        def do_POST(self):
            if self.path.split("?")[0] != path:
                return self._send(404, b'{"error":"not found"}')
            if not self._host_ok():
                return self._send(403, b'{"error":"loopback only"}')
            length = int(self.headers.get("Content-Length") or 0)
            if length <= 0 or length > MAX_BODY:
                return self._send(413 if length > MAX_BODY else 400, b'{"error":"bad body"}')
            try:
                message = json.loads(self.rfile.read(length))
            except ValueError:
                return self._send(400, json.dumps(error(None, -32700, "parse error")).encode())
            if isinstance(message, list):
                replies = [r for r in (tools.handle(m) for m in message) if r is not None]
                if not replies:
                    return self._send(202)
                return self._send(200, json.dumps(replies).encode())
            reply = tools.handle(message)
            if reply is None:
                return self._send(202)
            self._send(200, json.dumps(reply).encode())

    return Handler
