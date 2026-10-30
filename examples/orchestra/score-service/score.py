"""The score: a MIDI file read into one line per chair, cut into phrases.

A line is the notes one chair plays, in ticks from the start, as (tick, on,
key, velocity) sorted by time, with every note-off before a note-on at the
same tick. The chairs of a string section share their section's line, as
players in unison do; a section may give each chair a line of its own (a
track, or the upper or lower note of the track's chords: Oboe I and II). The
file's own volume and program changes are dropped: the orchestra sets those
itself, from who is playing.

A phrase is a fixed group of bars, numbered from 1, the same bars in every
line: phrase 1 is bars 1–8, phrase 2 bars 9–16. Each line's phrase says what
it holds (notes, rests, range), which is what a chair weighs when
it chooses the phrase it plays next.
"""

import mido

NAMES = ["C", "C#", "D", "Eb", "E", "F", "F#", "G", "Ab", "A", "Bb", "B"]


class Score:
    def __init__(self, path, sections, phrase_bars=8):
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

        def notes_of(section, track, voice=None):
            notes = []
            for name in track if isinstance(track, list) else [track]:
                if name not in tracks:
                    raise ValueError(f"section {section['id']}: no track {name!r} in {path}")
                notes += read_notes(tracks[name], section.get("transpose", 0))
            notes.sort(key=lambda n: (n[0], n[1]))
            return split_voice(notes, voice) if voice else notes

        self.parts, self.lines, self.line_names = {}, {}, {}
        for section in sections:
            self.parts[section["id"]] = notes_of(section, section["track"])
            lines = section.get("lines") or []
            for n, line in enumerate(lines, start=1):
                line_id = f"{section['id']}-{n}"
                self.lines[line_id] = notes_of(section, line.get("track", section["track"]), line.get("voice"))
                self.line_names[line_id] = line["name"]
            if not lines:
                self.lines[section["id"]] = self.parts[section["id"]]
                self.line_names[section["id"]] = section["name"]
        self.end_tick = max(n[0] for notes in self.parts.values() for n in notes)
        self.bars = -(-self.end_tick // self.ticks_per_bar)
        self.phrase_bars = phrase_bars
        self.phrase_count = -(-self.bars // phrase_bars)
        self.phrase_table = {line: describe(self, notes) for line, notes in self.lines.items()}

    def bar_of(self, tick):
        """The bar a tick falls in, counted from 1 (0 and below in a count-in)."""
        return int(tick // self.ticks_per_bar) + 1

    def tick_of(self, bar):
        """The first tick of a bar counted from 1."""
        return (bar - 1) * self.ticks_per_bar

    def phrase_of(self, bar):
        """The phrase a bar belongs to."""
        return (max(1, bar) - 1) // self.phrase_bars + 1

    def phrase_span(self, phrase):
        """The first and last bar of a phrase."""
        first = (phrase - 1) * self.phrase_bars + 1
        return first, min(self.bars, first + self.phrase_bars - 1)


def describe(score, notes):
    """Each phrase of a line: its bars, its notes, the bars it rests and its range."""
    out = []
    for phrase in range(1, score.phrase_count + 1):
        first, last = score.phrase_span(phrase)
        start, end = score.tick_of(first), score.tick_of(last + 1)
        ons = [n for n in notes if n[1] and start <= n[0] < end]
        sounding = {score.bar_of(n[0]) for n in ons}
        keys = [n[2] for n in ons]
        out.append({"phrase": phrase, "bars": [first, last], "notes": len(ons), "restBars": (last - first + 1) - len(sounding),
                    "range": f"{name(min(keys))}–{name(max(keys))}" if keys else ""})
    return out


def name(key):
    return f"{NAMES[key % 12]}{key // 12 - 1}"


def read_notes(track, transpose):
    notes, now = [], 0
    for message in track:
        now += message.time
        if message.type == "note_on" and message.velocity > 0:
            notes.append((now, True, clamp(message.note + transpose), message.velocity))
        elif message.type in ("note_off", "note_on"):
            notes.append((now, False, clamp(message.note + transpose), 0))
    return notes


def split_voice(notes, voice):
    """One player of a track that holds two in chords: the upper or the lower note of each chord; a single note is both's."""
    spans, held = [], {}
    for tick, on, key, velocity in notes:
        if on:
            held.setdefault(key, []).append((tick, velocity))
        elif held.get(key):
            start, velocity = held[key].pop(0)
            spans.append((start, tick, key, velocity))
    chords = {}
    for span in spans:
        chords.setdefault(span[0], []).append(span)
    kept = [max(c, key=lambda s: s[2]) if voice == "upper" else min(c, key=lambda s: s[2]) for c in chords.values()]
    out = [(s, True, k, v) for s, _, k, v in kept] + [(e, False, k, 0) for _, e, k, _ in kept]
    out.sort(key=lambda n: (n[0], n[1]))
    return out


def clamp(key):
    return max(0, min(127, key))
