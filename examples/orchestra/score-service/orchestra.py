"""The orchestra: the clock, the chairs, and what each chair asked for.

Every chair belongs to a section; a section plays one part of the score. A
chair asks to enter at a bar, to play softer or louder, or to stop; the
section sounds while at least one of its chairs is playing, as loud as the
share of its chairs playing times their mean dynamic, so eight violins
down to seven is a step in level you hear. The notes and the clock are the
sequencer's: render(frames) advances the clock by that much audio and hands
the synthesizer the notes that fall in it.

Nothing here talks to the network: the server calls enter, dynamics, stop,
listen and snapshot for the bots and the page, and the bus calls stop_all
and set_tempo for the operator's events.
"""

import threading
import time

WAITING, SCHEDULED, PLAYING, STOPPED = "waiting", "scheduled", "playing", "stopped"


class Refused(Exception):
    """A request the orchestra will not do; the bot reads it as the tool's error."""


class Orchestra:
    def __init__(self, score, config, synth, clock=time.time):
        self.score, self.synth, self.clock = score, synth, clock
        self.rate = config["audio"]["rate"]
        self.levels = config["dynamics"]
        self.tempo_range = (config["tempo"]["min"], config["tempo"]["max"])
        self.sections = {s["id"]: s for s in config["sections"]}
        self.channel = {}
        for i, s in enumerate(config["sections"]):
            self.channel[s["id"]] = i if i < 9 else i + 1
            synth.program(self.channel[s["id"]], s["program"])
        self.chairs = {f"{s['id']}-{n}": {"section": s["id"]} for s in config["sections"] for n in range(1, s["chairs"] + 1)}
        self.cond = threading.Condition()
        self.status, self.take, self.bpm, self.tick = "idle", 0, score.bpm, 0.0
        self.version, self.facts, self.cut, self.frames = 0, [], None, 0
        self._reset_chairs()

    # --- the bots' requests -------------------------------------------------

    def enter(self, chair, bar=None, dynamics=None):
        with self.cond:
            c = self._chair(chair)
            if dynamics is not None:
                self._level(dynamics)
            note = ""
            if self.status != "playing":
                self._start_take()
                target = max(1, int(bar or 1))
            else:
                now = self.score.bar_of(self.tick)
                target = now + 1
                if bar is not None and int(bar) >= target:
                    target = int(bar)
                elif bar is not None:
                    note = f"bar {int(bar)} had passed; "
            if target > self.score.bars:
                raise Refused(f"the movement has {self.score.bars} bars; bar {target} is past its end")
            c.update(state=SCHEDULED, entryTick=self.score.tick_of(target), enteredBar=target, dynamics=dynamics or c["dynamics"])
            self._fact("enter", chair, bar=target, dynamics=c["dynamics"])
            seconds = max(0.0, (self.score.tick_of(target) - self.tick) / self._ticks_per_second())
            return {"chair": chair, "enteredAt": target, "now": self.score.bar_of(self.tick), "inSeconds": round(seconds, 1),
                    "dynamics": c["dynamics"], "note": note + f"you come in at bar {target}"}

    def dynamics(self, chair, level):
        with self.cond:
            c = self._chair(chair)
            self._level(level)
            c["dynamics"] = level
            self._fact("dynamics", chair, dynamics=level)
            self._apply_volume(c["section"])
            return {"chair": chair, "dynamics": level, "state": c["state"]}

    def stop(self, chair):
        with self.cond:
            c = self._chair(chair)
            was = c["state"]
            c["state"] = STOPPED
            self._fact("stop", chair)
            self._apply_volume(c["section"])
            return {"chair": chair, "state": STOPPED, "was": was, "bar": self.score.bar_of(self.tick)}

    def listen(self, chair, bars=8, max_seconds=20):
        """Wait until bars pass, the chair is stopped or the music ends; then say where things are."""
        deadline = self.clock() + max(1, min(max_seconds, 60))
        with self.cond:
            c = self._chair(chair)
            take, start = self.take, self.score.bar_of(self.tick)
            while True:
                if self.take != take or self.status != "playing" or c["state"] == STOPPED:
                    break
                if self.score.bar_of(self.tick) >= start + max(1, int(bars)):
                    break
                left = deadline - self.clock()
                if left <= 0:
                    break
                self.cond.wait(min(left, 0.5))
            return self._reading(chair)

    # --- the operator's events (from the bus) ---------------------------------

    def stop_all(self, why, event_at=None):
        with self.cond:
            if self.status != "playing":
                return None
            for section in self.sections:
                self.synth.silence(self.channel[section])
            for c in self.chairs.values():
                if c["state"] in (SCHEDULED, PLAYING):
                    c["state"] = STOPPED
            self.status = "stopped"
            cut_at = self.clock()
            self.cut = {"why": why, "bar": self.score.bar_of(self.tick), "frame": self.frames, "cutAt": cut_at, "eventAt": event_at,
                        "afterEventSeconds": None if event_at is None else round(cut_at - event_at, 3)}
            self._fact("stop.all", None, why=why, afterEventSeconds=self.cut["afterEventSeconds"])
            return self.cut

    def heard(self, name, message, data, at):
        """An operator's event on the bus, kept for the page as it was published."""
        with self.cond:
            self._fact("bus", None, name=name, message=message, data=data, eventAt=at)

    def density(self):
        """Each part's note-ons per bar, scaled to its own busiest bar: the score strip of the page."""
        out = []
        for sid, s in self.sections.items():
            counts = [0] * self.score.bars
            for tick, on, _, _ in self.score.parts[sid]:
                if on:
                    counts[min(self.score.bar_of(tick), self.score.bars) - 1] += 1
            top = max(counts) or 1
            out.append({"id": sid, "name": s["name"], "density": [round(c / top, 2) for c in counts]})
        return {"bars": self.score.bars, "beatsPerBar": self.score.beats_per_bar, "sections": out}

    def set_tempo(self, bpm):
        with self.cond:
            low, high = self.tempo_range
            self.bpm = float(max(low, min(high, bpm)))
            self._fact("tempo", None, bpm=self.bpm)
            return self.bpm

    # --- the sequencer --------------------------------------------------------

    def render(self, frames):
        """Advance the clock by frames of audio; the synthesizer gets the notes in them."""
        with self.cond:
            if self.status == "playing":
                start, end = self.tick, self.tick + frames / self.rate * self._ticks_per_second()
                bar_before = self.score.bar_of(start)
                for chair, c in self.chairs.items():
                    if c["state"] == SCHEDULED and c["entryTick"] < end:
                        c["state"] = PLAYING
                        self._fact("playing", chair, bar=self.score.bar_of(c["entryTick"]))
                        self._apply_volume(c["section"])
                for section, notes in self.score.parts.items():
                    self._play(section, notes, start, end)
                self.tick = end
                if self.score.bar_of(end) != bar_before:
                    self._changed()
                if end > self.score.end_tick + self.score.ticks_per_beat:
                    self.status = "ended"
                    for section in self.sections:
                        self.synth.all_off(self.channel[section])
                    self._fact("ended", None)
            self.frames += frames
        return self.synth.samples(frames)

    def snapshot(self):
        with self.cond:
            sections = []
            for sid, s in self.sections.items():
                chairs = [{"id": k, "state": c["state"], "dynamics": c["dynamics"], "enteredBar": c.get("enteredBar")}
                          for k, c in self.chairs.items() if c["section"] == sid]
                sections.append({"id": sid, "name": s["name"], "family": s["family"], "chairs": chairs,
                                 "playing": sum(1 for c in chairs if c["state"] == PLAYING)})
            return {"version": self.version, "status": self.status, "take": self.take, "bar": self.score.bar_of(self.tick),
                    "bars": self.score.bars, "beat": round((self.tick % self.score.ticks_per_bar) / self.score.ticks_per_beat, 2),
                    "bpm": self.bpm, "sections": sections, "cut": self.cut, "facts": self.facts[-30:],
                    "lastBus": next((f for f in reversed(self.facts) if f["what"] == "bus"), None)}

    def wait_change(self, version, seconds):
        with self.cond:
            self.cond.wait_for(lambda: self.version != version, timeout=seconds)
        return self.snapshot()

    # --- inside -------------------------------------------------------------

    def _play(self, section, notes, start, end):
        cursor = self.cursor[section]
        channel, live = self.channel[section], self._active(section) > 0
        while cursor < len(notes) and notes[cursor][0] < end:
            tick, on, key, velocity = notes[cursor]
            if tick >= start or not on:
                if on and live:
                    self.synth.note_on(channel, key, velocity)
                elif not on:
                    self.synth.note_off(channel, key)
            cursor += 1
        self.cursor[section] = cursor

    def _active(self, section):
        return sum(1 for c in self.chairs.values() if c["section"] == section and c["state"] == PLAYING)

    def _apply_volume(self, section):
        playing = [c for c in self.chairs.values() if c["section"] == section and c["state"] == PLAYING]
        total = self.sections[section]["chairs"]
        if not playing:
            self.synth.all_off(self.channel[section])
            self.synth.volume(self.channel[section], 0)
            return
        # The synthesizer's volume is a square law (amplitude = (cc/127)²); this
        # makes a section's amplitude its mean dynamic times the square root of
        # its share of chairs playing, as players that are not in phase add up:
        # one violin of ten is 10 dB under the ten, eight to seven 0.6 dB.
        mean = sum(self.levels[c["dynamics"]] for c in playing) / len(playing)
        self.synth.volume(self.channel[section], round(127 * (mean ** 0.5) * (len(playing) / total) ** 0.25))

    def _start_take(self):
        self.take += 1
        self.status, self.tick, self.cut = "playing", 0.0, None
        self.cursor = {s: 0 for s in self.score.parts}
        self._reset_chairs()
        for section in self.sections:
            self.synth.all_off(self.channel[section])
            self.synth.volume(self.channel[section], 0)
        self._fact("take", None, take=self.take)

    def _reset_chairs(self):
        self.cursor = {s: 0 for s in self.score.parts}
        for c in self.chairs.values():
            c.update(state=WAITING, dynamics="mf", entryTick=None, enteredBar=None)

    def _reading(self, chair):
        c = self.chairs[chair]
        s = self.sections[c["section"]]
        playing = self._active(c["section"])
        return {"chair": chair, "state": c["state"], "dynamics": c["dynamics"], "status": self.status,
                "bar": self.score.bar_of(self.tick), "bars": self.score.bars, "bpm": self.bpm,
                "section": f"{s['name']}: {playing} of {s['chairs']} playing"}

    def _chair(self, chair):
        if chair not in self.chairs:
            raise Refused(f"there is no chair {chair!r}")
        return self.chairs[chair]

    def _level(self, level):
        if level not in self.levels:
            raise Refused(f"dynamics is one of {', '.join(self.levels)}")

    def _ticks_per_second(self):
        return self.bpm / 60.0 * self.score.ticks_per_beat

    def _fact(self, what, chair, **data):
        self.facts.append({"at": round(self.clock(), 3), "what": what, "chair": chair, "bar": self.score.bar_of(self.tick), **data})
        del self.facts[:-200]
        self._changed()

    def _changed(self):
        self.version += 1
        self.cond.notify_all()
