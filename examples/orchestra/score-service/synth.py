"""The synthesizer, and the audio stream it feeds.

FluidSynth renders into memory (no sound card): the render loop asks the
orchestra for one block at a time, paced by the wall clock, and hands each
block to every listener of the stream and to the recording, if any. A
stream is a WAV with an open-ended length, so OBS, ffplay or a browser can
take it as it comes.
"""

import os
import struct
import threading
import time
import wave


def soundfont(candidates):
    for path in candidates:
        if os.path.exists(os.path.expanduser(path)):
            return os.path.expanduser(path)
    raise FileNotFoundError("no soundfont: install one of " + ", ".join(candidates))


class FluidSynth:
    def __init__(self, path, rate):
        import fluidsynth

        self.fs = fluidsynth.Synth(samplerate=float(rate), gain=0.2)
        self.font = self.fs.sfload(path)
        if self.font < 0:
            raise RuntimeError(f"FluidSynth could not load {path}")
        self.path = path

    def program(self, channel, program):
        self.fs.program_select(channel, self.font, 0, program)

    def note_on(self, channel, key, velocity):
        self.fs.noteon(channel, key, velocity)

    def note_off(self, channel, key):
        self.fs.noteoff(channel, key)

    def volume(self, channel, value):
        self.fs.cc(channel, 7, max(0, min(127, value)))

    def all_off(self, channel):
        self.fs.cc(channel, 123, 0)  # all notes off: each note's own release

    def silence(self, channel):
        self.fs.cc(channel, 120, 0)  # all sound off: no release, silence now

    def samples(self, frames):
        return self.fs.get_samples(frames).tobytes()


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


def run(orchestra, stream, rate, block, record=None, stop=None, ahead=0.05):
    """Render forever (or until stop is set), one block at a time, at the speed of the wall clock."""
    out = None
    if record:
        out = wave.open(record, "wb")
        out.setnchannels(2), out.setsampwidth(2), out.setframerate(rate)
    period, due = block / rate, time.monotonic()
    try:
        while not (stop and stop.is_set()):
            data = orchestra.render(block)
            stream.publish(data)
            if out:
                out.writeframes(data)
            due += period
            delay = due - ahead - time.monotonic()
            if delay > 0:
                time.sleep(delay)
            elif delay < -1:
                due = time.monotonic()
    finally:
        if out:
            out.close()
