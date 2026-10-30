"""The orchestra: the clock, the chairs, and the phrases each chair asked for.

Every chair has a voice of its own: a synthesizer channel with its section's
instrument, its place on the stage (pan), and a few cents and milliseconds of
its own, so ten violins sound as ten and one violin as one. A chair plays its
line of the score phrase by phrase: it asks for a phrase (play) and only the
notes of the phrases it asked for sound, each at its own bars. Nothing sounds
that no chair asked for.

A chair chooses its next phrase while the current one plays: its fork opens
when its last phrase begins (some chairs a few beats later, so that 36 chairs
do not all ask at once), and phrases and listen answer with the candidates and
the question to put to Jev. The service never sees Jev: a chair's pick and its
probability are what the bot reported when it played, and are kept as such.

The notes and the clock are the sequencer's: render(frames) advances the clock
by that much audio and hands the synthesizer the notes that fall in it. A cue
from the conductor, or the first play, starts a take with a count-in, so every
chair cued can choose its first phrase before the music reaches it.

The conductor may also pause one group, or everyone, while the music goes on
(hold): its chairs fall silent at once and are paused, so a run still alive
cannot play until a cue names them. A paused chair's run is not told to stop:
listen keeps it waiting, and the cue that names it opens its next choice, so
the bot comes back in without starting a new run. A cue with dynamics while
the music plays sets that level on its chairs at once, and on the phrases they
already asked for.

Nothing here talks to the network: the server calls phrases, play, listen,
stop and snapshot for the bots and the page, and the bus calls cue, hold,
stop_all and set_tempo for the operator's events.
"""

import bisect
import json
import math
import threading
import time

WAITING, SCHEDULED, PLAYING, STOPPED, PAUSED = "waiting", "scheduled", "playing", "stopped", "paused"
JUDGE_SOURCE = "reported by the bot from its axlr_judge call; the service does not see Jev"
DYNAMICS_SOURCE = "the chair's last play, or the conductor's last cue with dynamics, applied at once"


class Refused(Exception):
    """A request the orchestra will not do; the bot reads it as the tool's error."""


class Orchestra:
    def __init__(self, score, config, synth, clock=time.time):
        self.score, self.synth, self.clock = score, synth, clock
        self.rate = config["audio"]["rate"]
        self.levels = config["dynamics"]
        self.tempo_range = (config["tempo"]["min"], config["tempo"]["max"])
        self.count_in = config.get("countIn", {}).get("bars", 4)
        phrases = config.get("phrases", {})
        self.candidates_wanted = phrases.get("candidates", 3)
        self.fork_spread = phrases.get("forkSpreadBeats", 8)
        voices = config.get("voices", {})
        self.sections = {s["id"]: s for s in config["sections"]}
        self.chairs, channel, index = {}, 0, 0
        for s in config["sections"]:
            total, low, high = s["chairs"], *(s.get("pan") or [0.0, 0.0])
            for n in range(1, total + 1):
                if channel % 16 == 9:  # General MIDI's drum channel in each bank of sixteen
                    channel += 1
                chair = f"{s['id']}-{n}"
                line = chair if s.get("lines") else s["id"]
                spread = 0.0 if total == 1 else (n - 1) / (total - 1)
                self.chairs[chair] = {
                    "section": s["id"], "n": n, "line": line, "channel": channel, "index": index,
                    "sharing": 1 if s.get("lines") else total,
                    "pan": round(low + (high - low) * spread, 3),
                    "cents": round(voices.get("cents", 0) * (2 * spread - 1), 2),
                    "onset": voices.get("onsetMs", 0) / 1000.0 * (((n - 1) * 3) % 5) / 4,
                    "forkBeats": (index * 5) % self.fork_spread if self.fork_spread else 0,
                }
                synth.program(channel, s["program"])
                synth.pan(channel, self.chairs[chair]["pan"])
                synth.bend(channel, self.chairs[chair]["cents"])
                synth.volume(channel, 0)
                channel, index = channel + 1, index + 1
        self.cond = threading.Condition()
        self.status, self.take, self.bpm, self.tick = "idle", 0, score.bpm, 0.0
        self.start_tick, self.version, self.facts, self.cut, self.frames = 0, 0, [], None, 0
        self.last_bus, self.cues, self.log, self.takes = None, {}, [], {}
        self._reset_chairs()

    # --- the bots' requests -------------------------------------------------

    def phrases(self, chair, bar=None):
        """The chair's state and the phrases it may play next, from the playhead or from bar."""
        with self.cond:
            self._chair(chair)
            if bar is not None and not 1 <= int(bar) <= self.score.bars:
                raise Refused(f"the movement has {self.score.bars} bars")
            return self._reading(chair, ask=True, bar=bar, full=True)

    def play(self, chair, phrase, dynamics=None, judge=None):
        """Schedule one phrase of the chair's line at its own bars; late, it is joined at the next bar."""
        with self.cond:
            c = self._chair(chair)
            if dynamics is not None:
                self._level(dynamics)
            if not isinstance(phrase, int) or not 1 <= phrase <= self.score.phrase_count:
                raise Refused(f"phrase is a number from 1 to {self.score.phrase_count}")
            first, last = self.score.phrase_span(phrase)
            if self.status == "stopped":
                raise Refused("the conductor stopped the music (stop.all): play nothing until the next cue")
            if self.status == "ended":
                raise Refused("the movement has ended")
            if c["held"]:
                raise Refused(f"the conductor paused your section ({c['held']}): play nothing; call listen and wait for your next cue")
            if self.status == "idle":
                self._start_take(first, why=f"{chair} played phrase {phrase}")
            if any(p["phrase"] == phrase for p in c["plan"]):
                raise Refused(f"phrase {phrase} is already yours; ask for the next with phrases")
            start, end = self.score.tick_of(first), self.score.tick_of(last + 1)
            join = self._join_bar(first, max(self.tick, self.start_tick))
            if join > last:
                raise Refused(f"phrase {phrase} (bars {first}–{last}) is over at bar {self._now_bar()}; "
                              f"choose among: {self._options(chair)}")
            late = join - max(first, self.score.bar_of(self.start_tick))
            lead = (start - self.tick) / self._ticks_per_second()
            entry = {"phrase": phrase, "bars": [first, last], "fromBar": join, "fromTick": self.score.tick_of(join), "endTick": end,
                     "dynamics": dynamics or c["dynamics"], "lateBars": late, "begun": False}
            c["plan"].append(entry)
            c["plan"].sort(key=lambda p: p["fromTick"])
            c["stopped"] = False
            c["dynamics"] = entry["dynamics"]
            if c["enteredBar"] is None:
                c["enteredBar"] = join
            report = self._judge_report(judge, phrase)
            if report is not None:
                c["judge"] = report
            record = {"at": round(self.clock(), 3), "take": self.take, "chair": chair, "phrase": phrase, "bars": [first, last], "fromBar": join,
                      "lateBars": late, "leadSeconds": round(lead, 2), "bar": self._now_bar(), "dynamics": entry["dynamics"],
                      "judge": report}
            self.log.append(dict(record, what="play"))
            self._fact("play", chair, phrase=phrase, bars=[first, last], fromBar=join, lateBars=late, dynamics=entry["dynamics"], judge=report)
            when = f"in {lead:.1f} s" if late == 0 else f"{late} bar{'s' if late > 1 else ''} late, from bar {join}"
            return {"chair": chair, "phrase": phrase, "bars": [first, last], "fromBar": join, "lateBars": late,
                    "inSeconds": round(max(0.0, lead), 1), "dynamics": entry["dynamics"],
                    "note": f"phrase {phrase} sounds {when}; call listen: your fork for the next phrase opens at bar {self._fork_bar(c)}"}

    def stop(self, chair):
        with self.cond:
            c = self._chair(chair)
            was = self._state(c)
            c["stopped"], c["plan"] = True, []
            self.synth.all_off(c["channel"])
            self._fact("stop", chair)
            return {"chair": chair, "state": STOPPED, "was": was, "bar": self.score.bar_of(self.tick), "done": True}

    def listen(self, chair, bars=8, max_seconds=20):
        """Wait until the chair's fork opens, bars pass, the chair is stopped or the music ends; then say where things are.

        A paused chair waits for the cue that names it, or the time given."""
        deadline = self.clock() + max(1, min(max_seconds, 60))
        with self.cond:
            c = self._chair(chair)
            take, start = self.take, self.score.bar_of(self.tick)
            while not self._fork_open(c):
                if self.take != take or self.status != "playing" or (c["stopped"] and not c["held"]):
                    break
                if not c["held"] and self.score.bar_of(self.tick) >= start + max(1, int(bars)):
                    break
                left = deadline - self.clock()
                if left <= 0:
                    break
                self.cond.wait(min(left, 0.5))
            # Stopped, or the movement over: the reading says so, and done. Paused is not stopped.
            return self._reading(chair, ask=self._fork_open(c) or self.status != "playing" or (c["stopped"] and not c["held"]))

    # --- the operator's events (from the bus) ---------------------------------

    def cue(self, name, data, at=None):
        """A conductor's cue: kept for the chairs' questions; when nothing plays, it starts a take with a count-in.

        While the music plays, it releases the chairs it names that a hold
        stopped, and a level in data.dynamics is theirs at once."""
        with self.cond:
            target = name[len("info.cue."):] if name.startswith("info.cue.") else name
            self.cues[target] = {"name": name, "data": data or {}, "heardAt": self.clock()}
            if self.status == "playing":
                named = self.chairs_of(target)
                for chair in named:
                    c = self.chairs[chair]
                    if c["held"]:  # paused: its run, still listening, may choose again
                        c["held"], c["stopped"] = None, False
                self.cond.notify_all()
                level = (data or {}).get("dynamics")
                if level in self.levels and named:
                    for chair in named:
                        c = self.chairs[chair]
                        c["dynamics"] = level
                        for p in c["plan"]:
                            if p["endTick"] > self.tick:
                                p["dynamics"] = level
                        if self._state(c) == PLAYING:
                            self._apply_volume(c, level)
                    self._fact("dynamics", None, target=target, dynamics=level, chairs=len(named))
                    return f"{level} for {len(named)} chairs"
                return None
            bar = (data or {}).get("bar")
            first = int(bar) if isinstance(bar, int) and 1 <= bar <= self.score.bars else 1
            self._start_take(first, why=name)
            return "count-in"

    def chairs_of(self, target):
        """The chairs a cue or a stop names: all, a family (strings, winds) or a section (vn1, ob…)."""
        if target == "all":
            return list(self.chairs)
        return [k for k, c in self.chairs.items() if target in (c["section"], self.sections[c["section"]]["family"])]

    def hold(self, chairs, why, event_at=None):
        """The conductor pauses some chairs, or all, while the music goes on: silent at once, paused until a cue names them."""
        with self.cond:
            if self.status != "playing":
                return None
            named = [k for k in chairs if k in self.chairs]
            for chair in named:
                c = self.chairs[chair]
                self.synth.silence(c["channel"])
                c["stopped"], c["held"], c["plan"] = True, why, []
            at = self.clock()
            self._fact("hold", None, why=why, chairs=named,
                       afterEventSeconds=None if event_at is None else round(at - event_at, 3))
            return {"held": named, "bar": self.score.bar_of(self.tick), "afterEventSeconds": None if event_at is None else round(at - event_at, 3)}

    def stop_all(self, why, event_at=None):
        with self.cond:
            if self.status != "playing":
                return None
            for c in self.chairs.values():
                self.synth.silence(c["channel"])
                if c["plan"] or self._state(c) != WAITING:
                    c["stopped"] = True
                c["plan"] = []
            self.status = "stopped"
            cut_at = self.clock()
            self.cut = {"why": why, "bar": self.score.bar_of(self.tick), "frame": self.frames, "cutAt": cut_at, "eventAt": event_at,
                        "afterEventSeconds": None if event_at is None else round(cut_at - event_at, 3)}
            self.takes.setdefault(self.take, {})["cut"] = self.cut
            self._fact("stop.all", None, why=why, afterEventSeconds=self.cut["afterEventSeconds"])
            return self.cut

    def heard(self, name, message, data, at):
        """An operator's event on the bus, kept for the page as it was published."""
        with self.cond:
            self.last_bus = {"at": round(self.clock(), 3), "what": "bus", "chair": None, "bar": self.score.bar_of(self.tick),
                             "name": name, "message": message, "data": data, "eventAt": at}
            self._fact("bus", None, name=name, message=message, data=data, eventAt=at)

    def set_tempo(self, bpm):
        with self.cond:
            low, high = self.tempo_range
            self.bpm = float(max(low, min(high, bpm)))
            self._fact("tempo", None, bpm=self.bpm)
            return self.bpm

    # --- the page -------------------------------------------------------------

    def density(self):
        """Each part's note-ons per bar, scaled to its own busiest bar: the score strip of the page; and each line's phrases."""
        out = []
        for sid, s in self.sections.items():
            counts = [0] * self.score.bars
            for tick, on, _, _ in self.score.parts[sid]:
                if on:
                    counts[min(self.score.bar_of(tick), self.score.bars) - 1] += 1
            top = max(counts) or 1
            out.append({"id": sid, "name": s["name"], "density": [round(c / top, 2) for c in counts]})
        lines = [{"id": line, "name": self.score.line_names[line], "chairs": [k for k, c in self.chairs.items() if c["line"] == line],
                  "phrases": table} for line, table in self.score.phrase_table.items()]
        return {"bars": self.score.bars, "beatsPerBar": self.score.beats_per_bar, "phraseBars": self.score.phrase_bars,
                "sections": out, "lines": lines}

    def snapshot(self):
        with self.cond:
            sections = []
            for sid, s in self.sections.items():
                chairs = [self._chair_view(k, c) for k, c in self.chairs.items() if c["section"] == sid]
                sections.append({"id": sid, "name": s["name"], "family": s["family"], "chairs": chairs,
                                 "playing": sum(1 for c in chairs if c["state"] == PLAYING)})
            counting = self.status == "playing" and self.tick < self.start_tick
            shown = max(self.tick, self.start_tick) if self.status == "playing" else self.tick
            return {"version": self.version, "status": self.status, "take": self.take, "bar": self.score.bar_of(shown),
                    "bars": self.score.bars, "beat": round((shown % self.score.ticks_per_bar) / self.score.ticks_per_beat, 2),
                    "countIn": self._count_in_left() if counting else 0,
                    "phraseBars": self.score.phrase_bars, "bpm": self.bpm, "sections": sections, "cut": self.cut,
                    "facts": self.facts[-30:], "lastBus": self.last_bus, "judgeSource": JUDGE_SOURCE, "dynamicsSource": DYNAMICS_SOURCE}

    def take_log(self, take=None):
        """Every play and every phrase that began in a take (this one by default), for the measurement."""
        with self.cond:
            take = self.take if take is None else int(take)
            out = {"take": take, "now": self.take, "status": self.status, "bar": self.score.bar_of(self.tick), "bpm": self.bpm,
                   "log": [e for e in self.log if e["take"] == take]}
            out.update(self.takes.get(take, {}))
            return out

    def wait_change(self, version, seconds):
        with self.cond:
            self.cond.wait_for(lambda: self.version != version, timeout=seconds)
        return self.snapshot()

    # --- the sequencer --------------------------------------------------------

    def render(self, frames):
        """Advance the clock by frames of audio; the synthesizer gets the notes of the phrases asked for in them."""
        with self.cond:
            if self.status == "playing":
                tps = self._ticks_per_second()
                start, end = self.tick, self.tick + frames / self.rate * tps
                for chair, c in self.chairs.items():
                    self._advance(chair, c, start, end, tps)
                self.tick = end
                if self.score.bar_of(end) != self.score.bar_of(start):
                    self._changed()
                elif int(end // self.score.ticks_per_beat) != int(start // self.score.ticks_per_beat):
                    self.cond.notify_all()  # a fork may have opened on this beat; the page need not hear of it
                if end > self.score.end_tick + self.score.ticks_per_beat:
                    self.status = "ended"
                    for c in self.chairs.values():
                        self.synth.all_off(c["channel"])
                    self._fact("ended", None)
            self.frames += frames
        return self.synth.samples(frames)

    # --- inside -------------------------------------------------------------

    def _advance(self, chair, c, start, end, tps):
        for p in c["plan"]:
            if not p["begun"] and p["fromTick"] < end and not c["stopped"]:
                p["begun"] = True
                self._apply_volume(c, p["dynamics"])
                self.log.append({"what": "began", "at": round(self.clock(), 3), "take": self.take, "chair": chair, "phrase": p["phrase"],
                                 "fromBar": p["fromBar"], "lateBars": p["lateBars"]})
                self._fact("playing", chair, phrase=p["phrase"], bar=p["fromBar"])
        notes, cursor, shift = self.score.lines[c["line"]], c["cursor"], c["onset"] * tps
        while cursor < len(notes) and notes[cursor][0] + shift < end:
            tick, on, key, velocity = notes[cursor]
            if not on:
                self.synth.note_off(c["channel"], key)
            elif tick + shift >= start and self._gate(c, tick):
                self.synth.note_on(c["channel"], key, velocity)
            cursor += 1
        c["cursor"] = cursor

    def _gate(self, c, tick):
        return not c["stopped"] and any(p["fromTick"] <= tick < p["endTick"] for p in c["plan"])

    def _state(self, c):
        if c["held"]:
            return PAUSED
        if c["stopped"]:
            return STOPPED
        if self.status == "playing":
            if any(p["fromTick"] <= self.tick < p["endTick"] for p in c["plan"]):
                return PLAYING
            if any(p["fromTick"] > self.tick for p in c["plan"]):
                return SCHEDULED
        return WAITING

    def _fork_tick(self, c):
        """When the chair should choose its next phrase: once its last phrase begins, a few beats later for some chairs."""
        if not c["plan"]:
            return None
        return c["plan"][-1]["fromTick"] + c["forkBeats"] * self.score.ticks_per_beat

    def _fork_bar(self, c):
        tick = self._fork_tick(c)
        return self.score.bar_of(tick) if tick is not None else self.score.bar_of(self.tick)

    def _fork_open(self, c):
        if self.status != "playing" or c["stopped"]:
            return False
        tick = self._fork_tick(c)
        if tick is not None and tick > self.tick:
            return False
        return bool(self._candidates(c))

    def _candidates(self, c, bar=None):
        """The phrases the chair may play next: after the last it asked for, or one already begun with two bars left."""
        if self.status == "playing":
            free = max([self.tick, self.start_tick] + [p["endTick"] for p in c["plan"]])
        else:
            free = self.score.tick_of(1)
        if bar is not None:
            free = max(free, self.score.tick_of(int(bar)))
        mine, out = {p["phrase"] for p in c["plan"]}, []
        for k in range(self.score.phrase_of(self.score.bar_of(free)), self.score.phrase_count + 1):
            first, last = self.score.phrase_span(k)
            start, end = self.score.tick_of(first), self.score.tick_of(last + 1)
            if k in mine or end <= free:
                continue
            join = self._join_bar(first, free)
            if join > last - 1:
                continue
            info = dict(self.score.phrase_table[c["line"]][k - 1])
            if self.status == "playing":
                info["startsInBars"] = round(max(0.0, (self.score.tick_of(join) - self.tick) / self.score.ticks_per_bar), 1)
            missed = join - max(first, self.score.bar_of(self.start_tick) if self.status == "playing" else 1)
            if missed > 0:
                info["late"] = f"begun: from bar {join}, missing {missed} bar{'s' if missed > 1 else ''}"
            out.append(info)
            if len(out) == self.candidates_wanted:
                break
        return out

    def _join_bar(self, first, free):
        """The bar a phrase from first is joined at when the chair is free from tick free: its first, or the next whole bar."""
        if self.score.tick_of(first) >= free:
            return first
        if free > self.tick and free % self.score.ticks_per_bar == 0:
            return self.score.bar_of(free)
        return self.score.bar_of(free) + 1

    def _options(self, chair):
        return "; ".join(f"phrase {p['phrase']} (bars {p['bars'][0]}–{p['bars'][1]})" for p in self._candidates(self.chairs[chair])) or "none left"

    def _ask(self, chair, candidates):
        """The question for Jev, as the chair passes it to axlr_judge: the state, the question, the options."""
        c = self.chairs[chair]
        s = self.sections[c["section"]]
        line = self.score.line_names[c["line"]]
        bpm, bar_seconds = round(self.bpm), self.score.beats_per_bar * 60.0 / self.bpm
        mine = [p for p in c["plan"] if p["endTick"] > self.tick]
        doing = "; ".join(f"phrase {p['phrase']} (bars {p['bars'][0]}–{p['bars'][1]}){' now' if p['fromTick'] <= self.tick else ' next'}" for p in mine)
        cue = self._cue_for(c)
        lines = [
            f"An orchestra plays Mozart's Symphony No. 25, first movement, {self.score.beats_per_bar}/4 at {bpm} bpm; "
            f"a phrase is {self.score.phrase_bars} bars, about {self.score.phrase_bars * bar_seconds:.1f} s.",
            f"This chair: {s['name']}, chair {c['n']} of {s['chairs']}; its line: {line}"
            + (" (the whole section plays it in unison)." if c["sharing"] > 1 else "."),
            f"The music is at bar {self.score.bar_of(max(self.tick, self.start_tick))} of {self.score.bars}"
            + (f", counting in ({self._count_in_left()} bars before bar {self.score.bar_of(self.start_tick)})" if self.tick < self.start_tick else "")
            + f". This chair has: {doing or 'no phrase scheduled'}.",
            f"Section: {s['name']}, {sum(1 for x in self.chairs.values() if x['section'] == c['section'] and self._state(x) == PLAYING)} of {s['chairs']} playing.",
            f"The conductor's last cue for this chair: {cue}.",
            "A chair plays its line in order, each phrase at its own bars; a phrase it does not play is a rest. "
            "A phrase already begun is joined at the next bar, missing its first bars.",
            "Candidates:",
        ]
        for p in candidates:
            lines.append(f"- phrase {p['phrase']}: bars {p['bars'][0]}–{p['bars'][1]}, "
                         + (f"starts in {p['startsInBars']} bars, " if p.get("startsInBars") is not None else "")
                         + (f"{p['late']}, " if p.get("late") else "")
                         + (f"{p['notes']} notes over {p['range']}, {p['restBars']} bars of rest" if p["notes"] else "all rest"))
        options = [f"phrase {p['phrase']}: bars {p['bars'][0]}–{p['bars'][1]}" for p in candidates]
        return {"state": "\n".join(lines), "question": f"Which phrase should {s['name']} chair {c['n']} play next?", "options": options}

    def _cue_for(self, c):
        s = self.sections[c["section"]]
        mine = [v for k, v in self.cues.items() if k in ("all", s["family"], s["id"])]
        if not mine:
            return "none"
        cue = max(mine, key=lambda v: v["heardAt"])
        return cue["name"] + (" " + json.dumps(cue["data"], ensure_ascii=False) if cue["data"] else "")

    def _now_bar(self):
        """The bar the music is at; during a count-in, the bar it will start at."""
        return self.score.bar_of(max(self.tick, self.start_tick) if self.status == "playing" else self.tick)

    def _count_in_left(self):
        return max(0, math.ceil((self.start_tick - self.tick) / self.score.ticks_per_bar))

    def _apply_volume(self, c, level):
        # The synthesizer's volume is a square law (amplitude = (cc/127)²). Each
        # chair of a section that plays in unison gets 1/√n of the level, so n
        # chairs, a few cents and milliseconds apart and not in phase, add up
        # to the section's level; one violin of ten is 10 dB under the ten.
        self.synth.volume(c["channel"], round(127 * (self.levels[level] ** 0.5) * c["sharing"] ** -0.25))

    def _start_take(self, first_bar, why):
        self.take += 1
        self.start_tick = self.score.tick_of(first_bar)
        self.status, self.cut = "playing", None
        self.tick = float(self.start_tick - self.count_in * self.score.ticks_per_bar)
        self.takes[self.take] = {"startBar": first_bar, "countInBars": self.count_in, "why": why, "startedAt": round(self.clock(), 3), "frame": self.frames}
        del self.log[:-20000]
        self._reset_chairs()
        for c in self.chairs.values():
            notes = self.score.lines[c["line"]]
            c["cursor"] = bisect.bisect_left(notes, (self.tick,))
            self.synth.all_off(c["channel"])
            self.synth.volume(c["channel"], 0)
        self._fact("take", None, take=self.take, startBar=first_bar, countInBars=self.count_in, why=why)

    def _reset_chairs(self):
        for c in self.chairs.values():
            c.update(stopped=False, held=None, dynamics="mf", plan=[], judge=None, enteredBar=None, cursor=0)

    def _chair_view(self, chair, c):
        now = next((p for p in c["plan"] if p["fromTick"] <= self.tick < p["endTick"]), None) if self.status == "playing" and not c["stopped"] else None
        nxt = next((p for p in c["plan"] if p["fromTick"] > self.tick), None) if self.status == "playing" and not c["stopped"] else None
        return {"id": chair, "state": self._state(c), "dynamics": c["dynamics"], "held": c["held"], "enteredBar": c["enteredBar"],
                "line": self.score.line_names[c["line"]],
                "phrase": {"playing": self._phrase_view(now), "next": self._phrase_view(nxt), "source": "the score service: the phrases the chair played"},
                "judge": c["judge"]}

    def _phrase_view(self, p):
        if p is None:
            return None
        out = {"phrase": p["phrase"], "bars": p["bars"], "fromBar": p["fromBar"], "lateBars": p["lateBars"], "dynamics": p["dynamics"]}
        if p["fromTick"] > self.tick:
            out["startsInBars"] = round((p["fromTick"] - self.tick) / self.score.ticks_per_bar, 1)
        return out

    def _judge_report(self, judge, phrase):
        """What the bot says Jev answered: kept as reported, with whether its choice names the phrase it played."""
        if not isinstance(judge, dict):
            return None
        out = {"phrase": phrase, "bar": self._now_bar(), "source": JUDGE_SOURCE}
        choice = judge.get("choice")
        if isinstance(choice, str) and choice.strip():
            out["choice"] = choice.strip()[:200]
            out["matchesPhrase"] = choice.strip().lower().startswith(f"phrase {phrase}") and not choice.strip()[len(f"phrase {phrase}"):][:1].isdigit()
        for key in ("probability", "confidence"):
            value = judge.get(key)
            if isinstance(value, (int, float)) and not isinstance(value, bool) and 0 <= value <= 1:
                out[key] = round(float(value), 3)
        return out if len(out) > 3 else None

    def _reading(self, chair, ask=False, bar=None, full=False):
        c = self.chairs[chair]
        s = self.sections[c["section"]]
        playing = sum(1 for x in self.chairs.values() if x["section"] == c["section"] and self._state(x) == PLAYING)
        view = self._chair_view(chair, c)
        out = {"chair": chair, "line": view["line"], "state": view["state"], "dynamics": c["dynamics"], "status": self.status,
               "bar": self.score.bar_of(max(self.tick, self.start_tick)) if self.status == "playing" else self.score.bar_of(self.tick),
               "countIn": self._count_in_left() if self.status == "playing" and self.tick < self.start_tick else 0,
               "bars": self.score.bars, "bpm": self.bpm, "section": f"{s['name']}: {playing} of {s['chairs']} playing",
               "playing": view["phrase"]["playing"], "next": view["phrase"]["next"]}
        if self.status == "playing" and not c["stopped"] and not self._fork_open(c):
            out["fork"] = f"your next choice opens at bar {self._fork_bar(c)}"
        # done: nothing more for this chair to choose in this take. A fixed
        # program (a Jev bot) reads it; a model reads the note. A paused chair
        # is never done: its run listens again until the cue that names it.
        if ask and self.status in ("stopped", "ended"):
            out["note"] = "the conductor stopped the music: wait for the next cue" if self.status == "stopped" else "the movement has ended"
            out["done"] = True
        elif c["held"]:
            out["note"] = f"the conductor paused your section ({c['held']}): call listen again; you choose again at your next cue"
        elif ask and c["stopped"]:
            out["note"] = "you stopped: wait for the next cue"
            out["done"] = True
        elif ask:
            candidates = self._candidates(c, bar)
            out["candidates"] = candidates if full else [{k: v for k, v in p.items() if k in ("phrase", "bars", "startsInBars", "late")} for p in candidates]
            if candidates:
                out["ask"] = self._ask(chair, candidates)
            else:
                out["note"] = "no phrase left to play"
                out["done"] = True
        return out

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
        self.facts.append({"at": round(self.clock(), 3), "what": what, "chair": chair, "bar": self._now_bar(), **data})
        del self.facts[:-200]
        self._changed()

    def _changed(self):
        self.version += 1
        self.cond.notify_all()
