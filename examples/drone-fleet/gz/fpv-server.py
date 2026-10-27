#!/usr/bin/env python3
"""Serve the gz cameras' pictures over HTTP: the FPV as MJPEG, the last photo.

    fpv-server.py [--frames /frames] [--listen 0.0.0.0:8081]

gz sim saves each camera's pictures as PNG files into
/frames/<vehicle>/<camera>/ (the models' <save>); this server reads them.

  GET /fpv/<vehicle>             multipart/x-mixed-replace MJPEG, 5 frames a
                                 second, the newest FPV picture with its label
  GET /latest/<vehicle>.jpg      the newest nadir picture (the photo), JPEG
  GET /latest/<vehicle>-fpv.jpg  the newest FPV picture, JPEG
  GET /healthz                   ok

The newest picture is the one with the latest mtime that has not grown in
the last 50 ms: gz may still be writing a newer one. A thread keeps the 20
newest files of each folder and deletes the rest every 2 s.
"""

import argparse
import http.server
import io
import os
import re
import socketserver
import threading
import time

from PIL import Image, ImageDraw

LABEL = "FPV: simulator camera · rendered port model"
NAME = re.compile(r"[a-z0-9-]+")
KEEP = 20
STABLE_S = 0.05
FPS = 5


def newest_stable(folder, settle=STABLE_S):
    """The path of the newest picture in folder no one is writing, or None."""
    try:
        entries = [e for e in os.scandir(folder) if e.is_file() and e.name.endswith((".png", ".jpg"))]
    except FileNotFoundError:
        return None
    entries.sort(key=lambda e: e.stat().st_mtime, reverse=True)
    for e in entries[:3]:
        size = e.stat().st_size
        if size == 0:
            continue
        time.sleep(settle)
        try:
            if os.stat(e.path).st_size == size:
                return e.path
        except FileNotFoundError:
            continue
    return None


def prune(folder, keep=KEEP):
    """Delete all but the keep newest pictures of folder."""
    try:
        entries = [e for e in os.scandir(folder) if e.is_file()]
    except FileNotFoundError:
        return 0
    entries.sort(key=lambda e: e.stat().st_mtime, reverse=True)
    gone = 0
    for e in entries[keep:]:
        try:
            os.unlink(e.path)
            gone += 1
        except FileNotFoundError:
            pass
    return gone


def to_jpeg(path, label=None, quality=80):
    """The picture as JPEG bytes, the label drawn along its bottom."""
    with Image.open(path) as im:
        im = im.convert("RGB")
    if label:
        draw = ImageDraw.Draw(im)
        x0, y0, x1, y1 = draw.textbbox((0, 0), label)
        h = y1 - y0 + 8
        draw.rectangle((0, im.height - h, x1 - x0 + 16, im.height), fill=(0, 0, 0))
        draw.text((8, im.height - h + 4 - y0), label, fill=(255, 255, 255))
    buf = io.BytesIO()
    im.save(buf, "JPEG", quality=quality)
    return buf.getvalue()


def pruner(root, stop):
    while not stop.is_set():
        try:
            for vehicle in os.listdir(root):
                for camera in ("fpv", "nadir"):
                    prune(os.path.join(root, vehicle, camera))
        except FileNotFoundError:
            pass
        stop.wait(2)


class Handler(http.server.BaseHTTPRequestHandler):
    frames = "/frames"

    def log_message(self, fmt, *args):
        pass

    def _send(self, code, body, ctype="text/plain; charset=utf-8"):
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def _picture(self, vehicle, camera, label=None):
        root = os.path.realpath(self.frames)
        folder = os.path.realpath(os.path.join(root, vehicle, camera))
        if not NAME.fullmatch(vehicle) or not folder.startswith(root + os.sep):
            return None
        path = newest_stable(folder)
        if path is None:
            return None
        try:
            return to_jpeg(path, label)
        except (OSError, SyntaxError):
            return None  # deleted or cut short under us

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path == "/healthz":
            return self._send(200, b"ok\n")
        m = re.fullmatch(r"/latest/([a-z0-9-]+?)(-fpv)?\.jpg", path)
        if m and NAME.fullmatch(m.group(1)):
            jpeg = self._picture(m.group(1), "fpv" if m.group(2) else "nadir")
            if jpeg is None:
                return self._send(404, b"no picture yet\n")
            return self._send(200, jpeg, "image/jpeg")
        m = re.fullmatch(r"/fpv/([a-z0-9-]+)", path)
        if m:
            return self._mjpeg(m.group(1))
        return self._send(404, b"not found\n")

    def _mjpeg(self, vehicle):
        self.send_response(200)
        self.send_header("Content-Type", "multipart/x-mixed-replace; boundary=frame")
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        try:
            while True:
                t = time.monotonic()
                jpeg = self._picture(vehicle, "fpv", LABEL)
                if jpeg is not None:
                    self.wfile.write(b"--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n" % len(jpeg))
                    self.wfile.write(jpeg)
                    self.wfile.write(b"\r\n")
                    self.wfile.flush()
                time.sleep(max(0.0, 1.0 / FPS - (time.monotonic() - t)))
        except (BrokenPipeError, ConnectionResetError):
            pass


class Server(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True
    allow_reuse_address = True


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--frames", default="/frames")
    ap.add_argument("--listen", default="0.0.0.0:8081")
    args = ap.parse_args()
    host, port = args.listen.rsplit(":", 1)
    Handler.frames = args.frames
    stop = threading.Event()
    threading.Thread(target=pruner, args=(args.frames, stop), daemon=True).start()
    print(f"fpv-server: {args.frames} on {args.listen}", flush=True)
    Server((host, int(port)), Handler).serve_forever()


if __name__ == "__main__":
    main()
