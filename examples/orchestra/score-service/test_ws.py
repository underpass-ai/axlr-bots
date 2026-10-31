"""The headset's WebSocket: the frames, and the whole link on a real server with no headset, no Jev and no control plane."""

import io
import json
import os
import socket
import struct
import threading
import unittest
from http.server import ThreadingHTTPServer

import server
import ws
from test_hands import OPEN, hands
from test_score_service import CONFIG

MASK = b"\x01\x02\x03\x04"


def client_frames(*frames):
    return io.BytesIO(b"".join(frames))


def read_frame(f):
    b0, b1 = f.read(2)
    n = b1 & 0x7F
    if n == 126:
        n = struct.unpack(">H", f.read(2))[0]
    elif n == 127:
        n = struct.unpack(">Q", f.read(8))[0]
    return b0 & 0x0F, f.read(n)


class FrameTest(unittest.TestCase):
    def test_the_handshake_answers_the_rfc_example(self):
        self.assertEqual(ws.accept_key("dGhlIHNhbXBsZSBub25jZQ=="), "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=")

    def test_masked_text_in_plain_text_out(self):
        out = io.BytesIO()
        sock = ws.Socket(client_frames(ws.frame(ws.TEXT, "é posture".encode(), MASK)), out)
        self.assertEqual(sock.recv(), "é posture")
        sock.send_json({"type": "state", "n": 1})
        out.seek(0)
        self.assertEqual(read_frame(out), (ws.TEXT, b'{"type": "state", "n": 1}'))

    def test_a_long_message_in_two_frames(self):
        body = ("x" * 70000).encode()[:60000]
        first = bytearray(ws.frame(ws.TEXT, body[:200], MASK))
        first[0] &= 0x7F  # not final
        sock = ws.Socket(client_frames(bytes(first), ws.frame(ws.CONTINUATION, body[200:], MASK)), io.BytesIO())
        self.assertEqual(len(sock.recv()), 60000)

    def test_ping_gets_pong_and_close_ends_it(self):
        out = io.BytesIO()
        sock = ws.Socket(client_frames(ws.frame(ws.PING, b"hi", MASK), ws.frame(ws.CLOSE, b"\x03\xe8", MASK)), out)
        self.assertIsNone(sock.recv())
        out.seek(0)
        self.assertEqual(read_frame(out), (ws.PONG, b"hi"))
        self.assertEqual(read_frame(out)[0], ws.CLOSE)
        self.assertFalse(sock.open)

    def test_an_unmasked_or_oversized_frame_closes_the_link(self):
        out = io.BytesIO()
        self.assertIsNone(ws.Socket(client_frames(ws.frame(ws.TEXT, b"plain")), out).recv())
        out.seek(0)
        self.assertEqual(read_frame(out), (ws.CLOSE, struct.pack(">H", 1002)))
        out = io.BytesIO()
        self.assertIsNone(ws.Socket(client_frames(ws.frame(ws.TEXT, b"x" * (ws.MAX_MESSAGE + 1), MASK)), out).recv())
        out.seek(0)
        self.assertEqual(read_frame(out), (ws.CLOSE, struct.pack(">H", 1009)))

    def test_a_dropped_link_is_closed_not_an_error(self):
        sock = ws.Socket(client_frames(b"\x81"), io.BytesIO())
        self.assertIsNone(sock.recv())
        with self.assertRaises(ws.Closed):
            sock.send_json({})


class LinkTest(unittest.TestCase):
    def setUp(self):
        self.h, self.o, _, self.publish, _ = hands(left=("open", 0.95))
        self.httpd = ThreadingHTTPServer(("127.0.0.1", 0), server.make_handler(self.o, None, CONFIG, self.h))
        self.httpd.daemon_threads = True
        threading.Thread(target=self.httpd.serve_forever, daemon=True).start()
        self.port = self.httpd.server_address[1]

    def tearDown(self):
        self.httpd.shutdown()
        self.httpd.server_close()

    def upgrade(self, origin):
        s = socket.create_connection(("127.0.0.1", self.port), timeout=5)
        s.sendall((f"GET /ws/hands HTTP/1.1\r\nHost: localhost:{self.port}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
                   f"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\nOrigin: {origin}\r\n\r\n").encode())
        f = s.makefile("rb")
        status = f.readline().decode()
        while f.readline() not in (b"\r\n", b""):
            pass
        return s, f, status

    def message(self, f, kind):
        while True:
            opcode, payload = read_frame(f)
            m = json.loads(payload)
            if m["type"] == kind:
                return m

    def test_state_comes_down_a_posture_goes_up_and_its_reading_comes_down(self):
        s, f, status = self.upgrade(f"http://localhost:{self.port}")
        self.assertIn("101", status)
        self.assertEqual(self.message(f, "state")["state"]["status"], "idle")
        s.sendall(ws.frame(ws.TEXT, json.dumps({"type": "posture", "id": 7, "hands": {"left": OPEN}, "heldMs": 300}).encode(), MASK))
        # The reading and the state it changed come down in either order:
        # the state is pushed as soon as the cue is applied, the reading once Jev's answer is kept.
        answer, playing = None, False
        while answer is None or not playing:
            opcode, payload = read_frame(f)
            m = json.loads(payload)
            if m["type"] == "hands":
                answer = m
            elif m["type"] == "state" and m["state"]["status"] == "playing":
                playing = True  # pushed, not asked for
        self.assertEqual((answer["id"], answer["record"]["read"], answer["record"]["applied"]), (7, {"left": "open"}, "count-in"))
        self.assertEqual(self.publish.events, [("info.cue.strings", {"dynamics": "f"}, None)])
        s.close()

    def test_a_page_from_elsewhere_may_not_conduct(self):
        s, _, status = self.upgrade("http://localhost.example.com")
        self.assertIn("403", status)
        s.close()


if __name__ == "__main__":
    unittest.main()
