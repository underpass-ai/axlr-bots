"""The score: a MIDI file read into one part per section.

A part is the notes of the section's track (or tracks), in ticks from the
start, as (tick, on, key, velocity) sorted by time, with every note-off
before a note-on at the same tick. The file's own volume and program
changes are dropped: the orchestra sets those itself, from who is playing.
"""

import mido


class Score:
    def __init__(self, path, sections):
        midi = mido.MidiFile(path)
        self.ticks_per_beat = midi.ticks_per_beat
        self.beats_per_bar, self.bpm = 4, 120.0
        tracks = {t.name: t for t in midi.tracks}
        for message in midi.tracks[0]:
            if message.type == "time_signature":
                self.beats_per_bar = message.numerator * 4 // message.denominator
            elif message.type == "set_tempo":
                self.bpm = round(mido.tempo2bpm(message.tempo), 1)
        self.ticks_per_bar = self.ticks_per_beat * self.beats_per_bar
        self.parts, end = {}, 0
        for section in sections:
            names = section["track"] if isinstance(section["track"], list) else [section["track"]]
            notes = []
            for name in names:
                if name not in tracks:
                    raise ValueError(f"section {section['id']}: no track {name!r} in {path}")
                notes += read_notes(tracks[name], section.get("transpose", 0))
            notes.sort(key=lambda n: (n[0], n[1]))
            self.parts[section["id"]] = notes
            end = max([end] + [n[0] for n in notes])
        self.end_tick = end
        self.bars = -(-end // self.ticks_per_bar)

    def bar_of(self, tick):
        """The bar a tick falls in, counted from 1."""
        return int(tick // self.ticks_per_bar) + 1

    def tick_of(self, bar):
        """The first tick of a bar counted from 1."""
        return (bar - 1) * self.ticks_per_bar


def read_notes(track, transpose):
    notes, now = [], 0
    for message in track:
        now += message.time
        if message.type == "note_on" and message.velocity > 0:
            notes.append((now, True, clamp(message.note + transpose), message.velocity))
        elif message.type in ("note_off", "note_on"):
            notes.append((now, False, clamp(message.note + transpose), 0))
    return notes


def clamp(key):
    return max(0, min(127, key))
