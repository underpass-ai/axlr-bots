"""The sound, and the audio stream it feeds.

The sound is superdough, Strudel's synthesizer, in a Node process of its own
(strudel/sound.mjs): an AudioContext with no sound card that runs at the
speed of the wall clock, and a tap at its output that writes every block of
audio to its stdout as 16-bit stereo PCM. The service reads those blocks:
they are its clock (the frames read so far are the time), they go to every
listener of the stream and to the recording, and the hits the rave schedules
go back on the process's stdin, a little ahead of that clock.

A stream is a WAV with an open-ended length, so OBS, ffplay or a browser can
take it as it comes. Nothing here is the orchestra's: the rave decides what
sounds when; this file moves bytes.
"""

import json
import logging
import os
import struct
import subprocess
import threading
import wave

HERE = os.path.dirname(os.path.abspath(__file__))
STRUDEL = os.path.join(HERE, "strudel")
log = logging.getLogger("sound")


class Superdough:
    """The sound process: start it, read its blocks, send it hits."""

    def __init__(self, rate=44100, block=2048, master=0.6, cache=None, network=True, node="node", cwd=STRUDEL):
        self.rate, self.block, self.master_gain, self.cache, self.network, self.node, self.cwd = rate, block, master, cache, network, node, cwd
        self.process, self.sounds, self.samples, self.channels, self.header = None, (), "none", 2, 0
        self.lock = threading.Lock()
        self.sent, self.frames, self.offset, self.context_time = 0, 0, None, 0.0

    def start(self, timeout=120):
        argv = [self.node, "--import", "./register.mjs", "sound.mjs", "--rate", str(self.rate), "--block", str(self.block),
                "--master", str(self.master_gain)]
        if self.cache:
            argv += ["--cache", os.path.expanduser(self.cache)]
        if not self.network:
            argv.append("--no-network")
        self.process = subprocess.Popen(argv, cwd=self.cwd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=None, bufsize=0)
        timer = threading.Timer(timeout, self.process.kill)
        timer.start()
        try:
            line = self.process.stdout.readline()
        finally:
            timer.cancel()
        if not line.startswith(b"READY "):
            raise RuntimeError("the sound did not come up: " + line[:200].decode(errors="replace"))
        ready = json.loads(line[6:])
        self.rate, self.block, self.channels, self.header = ready["rate"], ready["block"], ready.get("channels", 2), ready.get("header", 0)
        self.sounds, self.samples = tuple(ready["sounds"]), ready.get("samples", "none")
        return ready

    @property
    def frame_bytes(self):
        return self.channels * 2

    def read(self):
        """One block of PCM, exactly; None when the sound is gone.

        Each block comes with the context's clock when it was written. The sound
        runs ahead of what it has handed over (its own buffers: 0.47 s on gx10,
        steady), so the service keeps the offset between that clock and its
        frames, and re-anchors only when the offset grows by more than 50 ms
        (the service fell behind: one hiccup, not a late hit every block after)."""
        want = self.header + self.block * self.frame_bytes
        data = b""
        while len(data) < want:
            chunk = self.process.stdout.read(want - len(data))
            if not chunk:
                return None
            data += chunk
        self.frames += self.block
        if self.header:
            self.context_time = struct.unpack("<d", data[:self.header])[0]
            measured = self.context_time - self.frames / self.rate
            if self.offset is None or measured > self.offset + 0.05:
                self.offset = measured
        return data[self.header:]

    def time(self, frames):
        """The context's clock at the sound's frame count frames, as the service may schedule against it."""
        return frames / self.rate + (self.offset or 0.0)

    def _send(self, message):
        with self.lock:
            try:
                self.process.stdin.write((json.dumps(message) + "\n").encode())
                self.process.stdin.flush()
                self.sent += 1
            except (BrokenPipeError, ValueError, OSError) as exc:
                log.warning("sound gone: %s", exc)

    def hit(self, value, at, dur, cps, cycle):
        self._send({"hit": value, "at": round(at, 5), "dur": round(dur, 5), "cps": round(cps, 6), "cycle": cycle})

    def mute(self, on=True):
        self._send({"mute": bool(on)})

    def master(self, gain):
        self._send({"master": float(gain)})

    def close(self):
        if self.process and self.process.poll() is None:
            self._send({"quit": True})
            try:
                self.process.wait(2)
            except subprocess.TimeoutExpired:
                self.process.kill()


def wav_header(rate, channels=2, bits=16, frames=None):
    """A WAV header; without frames, the largest length a header holds, for a live stream."""
    size = 0xFFFFFFFF - 36 if frames is None else frames * channels * bits // 8
    return (b"RIFF" + struct.pack("<I", min(size + 36, 0xFFFFFFFF)) + b"WAVEfmt " +
            struct.pack("<IHHIIHH", 16, 1, channels, rate, rate * channels * bits // 8, channels * bits // 8, bits) +
            b"data" + struct.pack("<I", size))


class Stream:
    """Blocks of audio to whoever listens; a listener too slow loses blocks, never the clock."""

    def __init__(self, depth=50):
        self.depth, self.listeners, self.lock = depth, [], threading.Lock()

    def listen(self):
        import queue

        q = queue.Queue(self.depth)
        with self.lock:
            self.listeners.append(q)
        return q

    def leave(self, q):
        with self.lock:
            if q in self.listeners:
                self.listeners.remove(q)

    def publish(self, block):
        import queue

        with self.lock:
            for q in self.listeners:
                try:
                    q.put_nowait(block)
                except queue.Full:
                    pass


def run(rave, sound, stream, record=None, stop=None):
    """Read the sound block by block (its pace is the wall clock's), tell the rave how far the time went, and pass the sound on."""
    out = None
    if record:
        out = wave.open(record, "wb")
        out.setnchannels(sound.channels), out.setsampwidth(2), out.setframerate(sound.rate)
    try:
        while not (stop and stop.is_set()):
            data = sound.read()
            if data is None:
                log.error("the sound process ended; the music stops here")
                rave.sound_gone()
                return
            rave.advance(len(data) // sound.frame_bytes)
            stream.publish(data)
            if out:
                out.writeframes(data)
    finally:
        if out:
            out.close()
