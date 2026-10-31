"""A WebSocket (RFC 6455) on the service's own HTTP server, standard library only.

The headset keeps one connection open instead of a request per posture and a
long poll for the state: its postures go up, the orchestra's state and Jev's
readings come down as soon as they change. Text messages only; a client's
frames are masked, the server's are not; a ping is answered with a pong; a
close, a broken link or a frame out of the rules ends it.
"""

import base64
import hashlib
import json
import struct
import threading

GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
MAX_MESSAGE = 65536
TEXT, CONTINUATION, BINARY, CLOSE, PING, PONG = 0x1, 0x0, 0x2, 0x8, 0x9, 0xA


def accept_key(key):
    """The handshake's answer to the client's Sec-WebSocket-Key."""
    return base64.b64encode(hashlib.sha1((key + GUID).encode()).digest()).decode()


def frame(opcode, payload, mask=None):
    """One final frame; masked when a mask is given (the client's side)."""
    n = len(payload)
    bit = 0x80 if mask else 0
    if n < 126:
        head = struct.pack(">BB", 0x80 | opcode, bit | n)
    elif n < 65536:
        head = struct.pack(">BBH", 0x80 | opcode, bit | 126, n)
    else:
        head = struct.pack(">BBQ", 0x80 | opcode, bit | 127, n)
    if mask:
        return head + mask + unmask(payload, mask)
    return head + payload


def unmask(payload, mask):
    return bytes(b ^ mask[i % 4] for i, b in enumerate(payload))


class Closed(Exception):
    pass


class Socket:
    """The server's end, over the request handler's rfile and wfile after the 101."""

    def __init__(self, rfile, wfile):
        self.rfile, self.wfile, self.lock, self.open = rfile, wfile, threading.Lock(), True

    def send_json(self, obj):
        self._write(TEXT, json.dumps(obj).encode())

    def recv(self):
        """The next text message, or None once the link is closed."""
        parts, size = [], 0
        try:
            while True:
                b0, b1 = self._read(2)
                fin, opcode, n = b0 & 0x80, b0 & 0x0F, b1 & 0x7F
                if n == 126:
                    n = struct.unpack(">H", self._read(2))[0]
                elif n == 127:
                    n = struct.unpack(">Q", self._read(8))[0]
                if not b1 & 0x80:  # a client must mask
                    return self.close(1002)
                if n > MAX_MESSAGE or size + n > MAX_MESSAGE:
                    return self.close(1009)
                mask = self._read(4)
                payload = unmask(self._read(n), mask)
                if opcode == CLOSE:
                    return self.close()
                if opcode == PING:
                    self._write(PONG, payload)
                    continue
                if opcode == PONG:
                    continue
                if opcode not in (TEXT, BINARY, CONTINUATION):
                    return self.close(1002)
                parts.append(payload)
                size += n
                if fin:
                    return b"".join(parts).decode("utf-8", "replace")
        except Closed:
            self.open = False
            return None

    def close(self, code=1000):
        try:
            self._write(CLOSE, struct.pack(">H", code))
        except Closed:
            pass
        self.open = False
        return None

    def _read(self, n):
        data = self.rfile.read(n) if n else b""
        if len(data) < n:
            raise Closed()
        return data

    def _write(self, opcode, payload):
        with self.lock:
            if not self.open:
                raise Closed()
            try:
                self.wfile.write(frame(opcode, payload))
                self.wfile.flush()
            except OSError:
                self.open = False
                raise Closed()
