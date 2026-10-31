"""The rave: the clock, the chairs, and the pattern each chair asked for.

Each chair is one part of a techno set (kick, bass, hats, lead) and plays a
Strudel pattern: one cycle of the pattern is one bar, and the pattern loops
until the chair asks for another or stops. A chair writes its pattern as
text; it is evaluated before it sounds (evaluator.py), and the events it
holds are scheduled, bar by bar, on the sound (synth.py). A new pattern
starts at the next bar boundary: what was playing stops there, what is new
starts there, and <a b> alternations stay in step because the cycle is the
absolute bar. A phrase (phrases.bars) is how often a chair chooses: its fork
opens a couple of bars before the phrase ends, and phrases and listen answer
with candidates for its role and the question to put to Jev. The service
never sees Jev: a chair's pick is what the bot reported when it played.

The clock is the sound's: advance(frames) is called with every block of
audio the sound produced, and schedules the events of the next slice of
time a little ahead of it (audio.lookaheadSeconds) on the sound's own
clock (synth.Superdough.time), so nothing is late. A
cue from the conductor, or the first play, starts a take with a count-in.
The conductor may fade a group out while the music goes on (hold); the cue
that names it fades it back in. stop.all silences everything at once, drops
every pattern and refuses play until the next cue.

Nothing here talks to the network: the server calls phrases, play, listen,
stop and snapshot for the bots and the page, and the bus calls cue, hold,
stop_all and set_tempo for the operator's events.
"""

import json
import math
import threading
import time

WAITING, SCHEDULED, PLAYING, STOPPED = "waiting", "scheduled", "playing", "stopped"
JUDGE_SOURCE = "reported by the bot from its axlr_judge call; the service does not see Jev"
DYNAMICS_SOURCE = "the chair's last play, or the conductor's last cue with dynamics, applied at once"
BEATS_PER_BAR = 4
ENERGY = {"p": 1, "mf": 2, "f": 3}


class Refused(Exception):
    """A request the rave will not do; the bot reads it as the tool's error."""


class Rave:
    def __init__(self, config, library, sound, evaluator, clock=time.time):
        self.sound, self.evaluator, self.clock = sound, evaluator, clock
        self.rate = sound.rate
        self.lookahead = config["audio"].get("lookaheadSeconds", 0.25)
        self.levels = config["dynamics"]
        self.tempo_range = (config["tempo"]["min"], config["tempo"]["max"])
        self.bpm = float(config["tempo"].get("bpm", 128))
        self.count_in = config.get("countIn", {}).get("bars", 2)
        self.fade_out = config.get("hold", {}).get("fadeSeconds", 1.5)
        self.fade_in = config.get("hold", {}).get("fadeInSeconds", 0.5)
        patterns = config.get("patterns", {})
        self.phrase_bars = patterns.get("bars", 8)
        self.candidates_wanted = patterns.get("candidates", 3)
        self.lead_bars = patterns.get("leadBars", 2)
        self.max_per_bar = patterns.get("maxPerBar", 128)
        self.horizon = patterns.get("horizonBars", 128)
        self.max_code = patterns.get("maxCodeChars", 2000)
        self.sounds = set(sound.sounds)
        self.library = {k: v for k, v in library.items() if isinstance(v, list)}
        self.by_id = {e["id"]: dict(e, role=role) for role, entries in self.library.items() for e in entries}
        self.sections = {s["id"]: s for s in config["sections"]}
        self.chairs, index = {}, 0
        for s in config["sections"]:
            total, low, high = s["chairs"], *(s.get("pan") or [0.0, 0.0])
            for n in range(1, total + 1):
                spread = 0.0 if total == 1 else (n - 1) / (total - 1)
                chair = f"{s['id']}-{n}"
                self.chairs[chair] = {"section": s["id"], "role": s["id"], "n": n, "index": index, "sound": s.get("sound"),
                                      "pan": round(low + (high - low) * spread, 3), "forkBeats": (index * 3) % BEATS_PER_BAR}
                index += 1
        self.cond = threading.Condition()
        self.status, self.take, self.beat, self.frames = "idle", 0, 0.0, 0
        self.version, self.facts, self.cut, self.last_bus, self.cues, self.log, self.takes = 0, [], None, None, {}, [], {}
        self.asked = 0
        self._reset_chairs(first=True)

    # --- the bots' requests -------------------------------------------------

    def phrases(self, chair, bar=None):
        """The chair's state and the candidates it may play next, with the question for Jev."""
        with self.cond:
            self._chair(chair)
            return self._reading(chair, ask=True, full=True)

    def play(self, chair, pattern=None, candidate=None, dynamics=None, judge=None):
        """Schedule a pattern on the chair from the next bar: a candidate by number, a library entry by id, or the bot's own code."""
        with self.cond:
            c = self._chair(chair)
            if dynamics is not None:
                self._level(dynamics)
            if self.status == "stopped":
                raise Refused("the conductor stopped the music (stop.all): play nothing until the next cue")
            entry = self._resolve(c, pattern, candidate)
            from_bar = 1 if self.status == "idle" else self._next_bar()
        # The evaluator runs outside the lock: the clock must not wait for Node.
        haps, per_bar = self._evaluate(entry["code"], from_bar, self.horizon)
        with self.cond:
            c = self._chair(chair)
            if self.status == "stopped":
                raise Refused("the conductor stopped the music (stop.all) meanwhile: play nothing until the next cue")
            if self.status == "idle":
                self._start_take(why=f"{chair} played {entry['id']}")
            from_bar = max(from_bar, self._next_bar())
            plan = {"id": entry["id"], "name": entry["name"], "code": entry["code"], "custom": entry.get("custom", False),
                    "fromBar": from_bar, "haps": haps, "i": 0, "horizon": from_bar + self.horizon, "perBar": per_bar, "extending": False}
            c["next"] = plan
            c["stopped"] = False
            c["dynamics"] = dynamics or c["dynamics"]
            c["choiceBar"] = from_bar + self.phrase_bars
            if c["enteredBar"] is None:
                c["enteredBar"] = from_bar
            report = self._judge_report(judge, entry, c)
            if report is not None:
                c["judge"] = report
            lead = self._seconds_until_bar(from_bar)
            record = {"at": round(self.clock(), 3), "take": self.take, "chair": chair, "pattern": entry["id"], "custom": plan["custom"],
                      "fromBar": from_bar, "leadSeconds": round(lead, 2), "bar": self._now_bar(), "dynamics": c["dynamics"], "judge": report}
            self.log.append(dict(record, what="play"))
            self._fact("play", chair, pattern=entry["id"], name=entry["name"], custom=plan["custom"], fromBar=from_bar, perBar=per_bar,
                       dynamics=c["dynamics"], judge=report)
            return {"chair": chair, "pattern": {"id": entry["id"], "name": entry["name"], "code": entry["code"], "custom": plan["custom"]},
                    "fromBar": from_bar, "inSeconds": round(max(0.0, lead), 1), "eventsPerBar": per_bar, "dynamics": c["dynamics"],
                    "note": f"{entry['id']} starts at bar {from_bar} and loops; call listen: your fork for the next pattern opens at bar {c['choiceBar'] - self.lead_bars}"}

    def stop(self, chair):
        with self.cond:
            c = self._chair(chair)
            was = self._state(c)
            c["stopped"], c["playing"], c["next"] = True, None, None
            self._fact("stop", chair)
            return {"chair": chair, "state": STOPPED, "was": was, "bar": self._now_bar(), "done": True}

    def listen(self, chair, bars=8, max_seconds=20):
        """Wait until the chair's fork opens, bars pass, the chair is stopped or the music stops; then say where things are."""
        deadline = self.clock() + max(1, min(max_seconds, 60))
        with self.cond:
            c = self._chair(chair)
            take, start = self.take, self._now_bar()
            if self._fork_open(c):  # already open: wait for the next bar, so a bot that asks again and again asks once a bar
                until = start + 1
                while self._now_bar() < until and self.take == take and self.status == "playing" and not c["stopped"]:
                    left = deadline - self.clock()
                    if left <= 0:
                        break
                    self.cond.wait(min(left, 0.5))
            while not self._fork_open(c):
                if self.take != take or self.status != "playing" or c["stopped"]:
                    break
                if self._now_bar() >= start + max(1, int(bars)):
                    break
                left = deadline - self.clock()
                if left <= 0:
                    break
                self.cond.wait(min(left, 0.5))
            return self._reading(chair, ask=self._fork_open(c) or self.status != "playing" or c["stopped"])

    # --- the operator's events (from the bus) ---------------------------------

    def cue(self, name, data, at=None):
        """A conductor's cue: when nothing plays, it starts a take with a count-in; while the music plays it releases
        the chairs it names that a hold faded out, and data.dynamics (p, mf, f) or data.gain is theirs at once."""
        with self.cond:
            target = name[len("info.cue."):] if name.startswith("info.cue.") else name
            data = data or {}
            self.cues[target] = {"name": name, "data": data, "heardAt": self.clock()}
            started = False
            if self.status != "playing":
                self._start_take(why=name)
                started = True
            named = self.chairs_of(target)
            for chair in named:
                c = self.chairs[chair]
                if c["held"]:
                    c["held"] = None
                    self._fade_to(c, 1.0, self.fade_in)
            level = data.get("dynamics")
            if level in self.levels and named:
                for chair in named:
                    self.chairs[chair]["dynamics"] = level
                self._fact("dynamics", None, target=target, dynamics=level, chairs=len(named))
            gain = data.get("gain")
            if isinstance(gain, (int, float)) and named:
                for chair in named:
                    self._fade_to(self.chairs[chair], max(0.0, min(1.0, float(gain))), self.fade_in)
            self._changed()
            return "count-in" if started else (f"{level} for {len(named)} chairs" if level in self.levels else "released")

    def chairs_of(self, target):
        """The chairs a cue or a stop names: all, a family (drums, synths) or a section (kick, bass…)."""
        if target == "all":
            return list(self.chairs)
        return [k for k, c in self.chairs.items() if target in (c["section"], self.sections[c["section"]]["family"])]

    def hold(self, chairs, why, event_at=None):
        """The conductor fades some chairs out while the music goes on: their patterns kept, until a cue names them."""
        with self.cond:
            if self.status != "playing":
                return None
            named = [k for k in chairs if k in self.chairs]
            for chair in named:
                c = self.chairs[chair]
                c["held"] = why
                self._fade_to(c, 0.0, self.fade_out)
            at = self.clock()
            after = None if event_at is None else round(at - event_at, 3)
            self._fact("hold", None, why=why, chairs=named, afterEventSeconds=after)
            return {"held": named, "bar": self._now_bar(), "afterEventSeconds": after}

    def fade(self, chairs, gain, seconds):
        with self.cond:
            gain = max(0.0, min(1.0, float(gain)))
            changed = 0
            for chair in chairs:
                c = self.chairs.get(chair)
                if c is not None and abs(c["gainTarget"] - gain) >= 0.01:
                    self._fade_to(c, gain, seconds)
                    changed += 1
            return changed

    def stop_all(self, why, event_at=None):
        with self.cond:
            if self.status != "playing":
                return None
            self.sound.mute(True)
            for c in self.chairs.values():
                if c["playing"] or c["next"]:
                    c["stopped"] = True
                c["playing"], c["next"] = None, None
            self.status = "stopped"
            cut_at = self.clock()
            self.cut = {"why": why, "bar": self._now_bar(), "frame": self.frames, "cutAt": cut_at, "eventAt": event_at,
                        "afterEventSeconds": None if event_at is None else round(cut_at - event_at, 3)}
            self.takes.setdefault(self.take, {})["cut"] = self.cut
            self._fact("stop.all", None, why=why, afterEventSeconds=self.cut["afterEventSeconds"])
            return self.cut

    def heard(self, name, message, data, at):
        with self.cond:
            self.last_bus = {"at": round(self.clock(), 3), "what": "bus", "chair": None, "bar": self._now_bar(),
                             "name": name, "message": message, "data": data, "eventAt": at}
            self._fact("bus", None, name=name, message=message, data=data, eventAt=at)

    def set_tempo(self, bpm):
        with self.cond:
            low, high = self.tempo_range
            self.bpm = float(max(low, min(high, bpm)))
            self._fact("tempo", None, bpm=self.bpm)
            return self.bpm

    def sound_gone(self):
        with self.cond:
            self.status = "ended"
            self._fact("ended", None, why="the sound process ended")

    # --- the page -------------------------------------------------------------

    def score(self):
        """What the pages read once: the bars (a set has no end), the beats per bar, the phrase, the sections, the library."""
        return {"bars": 1000000, "beatsPerBar": BEATS_PER_BAR, "phraseBars": self.phrase_bars,
                "sections": [{"id": sid, "name": s["name"], "family": s["family"], "density": []} for sid, s in self.sections.items()],
                "seating": {},  # no chart: the page stands the band on one arc ahead, in the order of the sections
                "lines": [{"id": role, "name": self.sections[role]["name"] if role in self.sections else role,
                           "chairs": [k for k, c in self.chairs.items() if c["role"] == role],
                           "patterns": [{"id": e["id"], "name": e["name"], "energy": e["energy"], "code": e["code"]} for e in entries]}
                          for role, entries in self.library.items()]}

    def snapshot(self):
        with self.cond:
            sections = []
            for sid, s in self.sections.items():
                chairs = [self._chair_view(k, c) for k, c in self.chairs.items() if c["section"] == sid]
                sections.append({"id": sid, "name": s["name"], "family": s["family"], "chairs": chairs,
                                 "playing": sum(1 for c in chairs if c["state"] == PLAYING)})
            shown = self._shown_beat()
            counting = self.status == "playing" and shown < 0
            return {"version": self.version, "status": self.status, "take": self.take, "bar": self._bar_of(shown), "bars": 1000000,
                    "beat": round(max(0.0, shown) % BEATS_PER_BAR, 2), "countIn": self._count_in_left() if counting else 0,
                    "phraseBars": self.phrase_bars, "bpm": self.bpm, "sections": sections, "cut": self.cut,
                    "facts": self.facts[-30:], "lastBus": self.last_bus, "judgeSource": JUDGE_SOURCE, "dynamicsSource": DYNAMICS_SOURCE,
                    "sound": {"samples": self.sound.samples, "rate": self.rate, "lookaheadSeconds": self.lookahead}}

    def take_log(self, take=None):
        with self.cond:
            take = self.take if take is None else int(take)
            out = {"take": take, "now": self.take, "status": self.status, "bar": self._now_bar(), "bpm": self.bpm,
                   "log": [e for e in self.log if e["take"] == take]}
            out.update(self.takes.get(take, {}))
            return out

    def wait_change(self, version, seconds):
        with self.cond:
            self.cond.wait_for(lambda: self.version != version, timeout=seconds)
        return self.snapshot()

    # --- the sequencer --------------------------------------------------------

    def advance(self, frames):
        """The sound produced frames more: schedule the events of the slice of time that far ahead of them."""
        with self.cond:
            seconds = frames / self.rate
            t0 = self.sound.time(self.frames) + self.lookahead  # the sound's clock at the start of this slice, plus the lookahead
            self.frames += frames
            self._fading(seconds)
            if self.status != "playing":
                return
            bps = self.bpm / 60.0
            b0, b1 = self.beat, self.beat + seconds * bps
            for chair, c in self.chairs.items():
                self._schedule(chair, c, b0, b1, t0, bps)
            self.beat = b1
            bar0, bar1 = self._bar_of(b0 - self.lookahead * bps), self._bar_of(b1 - self.lookahead * bps)
            if bar1 != bar0 or (b0 < 0 <= b1):
                for c in self.chairs.values():
                    if c["playing"] and bar1 >= c["choiceBar"]:
                        c["choiceBar"] += self.phrase_bars  # the phrase rolled with no new choice: the pattern loops on
                self._changed()
            elif int(b1) != int(b0):
                self.cond.notify_all()  # a fork may have opened on this beat

    # --- inside -------------------------------------------------------------

    def _schedule(self, chair, c, b0, b1, t0, bps):
        nxt = c["next"]
        if nxt is not None:
            start = (nxt["fromBar"] - 1) * BEATS_PER_BAR
            if start < b1:  # the boundary falls in this slice: the old pattern stops there, the new begins there
                self._hits(chair, c, c["playing"], b0, min(b1, start), b0, t0, bps)
                self._hits(chair, c, nxt, max(b0, start), b1, b0, t0, bps)
                was = c["playing"]
                c["playing"], c["next"] = nxt, None
                self.log.append({"what": "began", "at": round(self.clock(), 3), "take": self.take, "chair": chair, "pattern": nxt["id"],
                                 "fromBar": nxt["fromBar"], "replaced": was["id"] if was else None})
                self._fact("began", chair, pattern=nxt["id"], name=nxt["name"], fromBar=nxt["fromBar"], replaced=was["id"] if was else None)
                return
        self._hits(chair, c, c["playing"], b0, b1, b0, t0, bps)

    def _hits(self, chair, c, plan, lo, hi, b0, t0, bps):
        """The plan's onsets in beats [lo, hi), each at its second on the sound's clock: beat b0 is second t0."""
        if plan is None or hi <= lo:
            return
        haps, i = plan["haps"], plan["i"]
        while i < len(haps) and haps[i]["b"] * BEATS_PER_BAR < lo:
            i += 1
        plan["i"] = i
        while i < len(haps) and haps[i]["b"] * BEATS_PER_BAR < hi:
            h = haps[i]
            i += 1
            onset = h["b"] * BEATS_PER_BAR
            if onset < (plan["fromBar"] - 1) * BEATS_PER_BAR:
                continue
            gain = c["gain"] * self.levels.get(c["dynamics"], 1.0)
            if gain < 0.002:
                continue
            value = {k: v for k, v in h["v"].items() if k != "noteMidi"}
            value.setdefault("s", c["sound"])
            value["gain"] = float(value.get("gain", 1.0)) * gain
            value.setdefault("pan", round((c["pan"] + 1) / 2, 3))
            dur = max(0.02, (h["e"] - h["b"]) * BEATS_PER_BAR / bps)
            self.sound.hit(value, t0 + (onset - b0) / bps, dur, bps / BEATS_PER_BAR, math.floor(h["b"]))
            c["hits"] += 1
        plan["i"] = i
        if not plan["extending"] and plan["horizon"] - self._bar_of(hi) < 16:
            plan["extending"] = True
            threading.Thread(target=self._extend, args=(chair, plan), daemon=True).start()

    def _extend(self, chair, plan):
        """More bars of a pattern that goes on: evaluated outside the lock, appended under it."""
        try:
            haps, _ = self._evaluate(plan["code"], plan["horizon"], self.horizon)
        except Refused as exc:
            haps = []
            self._fact("extend-failed", chair, pattern=plan["id"], error=str(exc))
        with self.cond:
            plan["haps"].extend(haps)
            plan["horizon"] += self.horizon
            plan["extending"] = False

    def _evaluate(self, code, from_bar, bars):
        """The pattern's events over bars from from_bar, or why it is refused."""
        result = self.evaluator.pattern(code, from_bar - 1, bars, max_haps=self.max_per_bar * bars)
        if not result.get("ok"):
            raise Refused(f"pattern refused: {result.get('error', 'no answer')}")
        unknown = sorted({str(s).split(":")[0] for s in result.get("sounds", [])} - self.sounds)
        if unknown:
            raise Refused(f"no such sound: {', '.join(unknown)}; the sounds are {', '.join(sorted(self.sounds))}")
        per_bar = result.get("perCycle", 0)
        if per_bar > self.max_per_bar:
            raise Refused(f"{per_bar:g} events a bar is too many (at most {self.max_per_bar})")
        if not result.get("haps"):
            raise Refused("the pattern has no events: silence is stop")
        return result["haps"], round(per_bar, 2)

    def _resolve(self, c, pattern, candidate):
        if candidate is not None:
            if not c["candidates"]:
                raise Refused("no candidates were offered yet: call phrases first")
            try:
                n = int(candidate)
            except (TypeError, ValueError):
                raise Refused("candidate is the number of one of the candidates offered")
            for e in c["candidates"]:
                if e["n"] == n:
                    return e
            raise Refused(f"candidate {n} was not offered; the candidates are {', '.join(str(e['n']) for e in c['candidates'])}")
        if not isinstance(pattern, str) or not pattern.strip():
            raise Refused("pattern is Strudel code (or candidate, the number of one offered)")
        pattern = pattern.strip()
        if pattern in self.by_id:
            e = self.by_id[pattern]
            if e["role"] != c["role"]:
                raise Refused(f"{pattern} is a {e['role']} pattern; you play the {c['role']}")
            return dict(e)
        for e in c["candidates"]:
            if e["code"] == pattern:
                return e
        if len(pattern) > self.max_code:
            raise Refused(f"a pattern has at most {self.max_code} characters")
        c["owned"] += 1
        return {"id": f"{c['role']}-own-{c['owned']}", "name": "the bot's own pattern", "code": pattern, "custom": True}

    def _candidates(self, c):
        entries = self.library.get(c["role"], [])
        if not entries:
            return []
        energy = ENERGY.get(c["dynamics"], 2)
        playing = c["playing"]["id"] if c["playing"] else None
        pool = [e for e in entries if abs(e["energy"] - energy) <= 1 and e["id"] != playing] or [e for e in entries if e["id"] != playing] or entries
        start = (c["asked"] * self.candidates_wanted) % len(pool)
        picked = [pool[(start + k) % len(pool)] for k in range(min(self.candidates_wanted, len(pool)))]
        return [{"n": k + 1, "id": e["id"], "name": e["name"], "energy": e["energy"], "code": e["code"]} for k, e in enumerate(picked)]

    def _reading(self, chair, ask, full=False):
        c = self.chairs[chair]
        bar = self._now_bar()
        out = {"chair": chair, "role": c["role"], "state": self._state(c), "status": self.status, "take": self.take, "bar": bar,
               "countIn": self._count_in_left() if self.status == "playing" and self._shown_beat() < 0 else 0,
               "bpm": self.bpm, "phraseBars": self.phrase_bars, "dynamics": c["dynamics"], "gain": round(c["gain"], 2), "held": c["held"],
               "pattern": self._pattern_view(c), "section": self._section_summary(c["section"]), "cue": self._cue_for(c)}
        if self.status == "stopped":
            out["done"] = True
            out["note"] = "the conductor stopped the music (stop.all): call stop, then nothing until the next cue"
            return out
        if self.status == "ended":
            out["done"] = True
            out["note"] = "the music ended"
            return out
        if c["stopped"]:
            out["done"] = True
            out["note"] = "you stopped; a cue that names you may call you back"
            return out
        if ask:
            c["candidates"] = self._candidates(c)
            c["asked"] += 1
            self.asked += 1
            out["candidates"] = c["candidates"]
            out["ask"] = self._ask(chair, c, out["candidates"])
            out["note"] = (f"choose with axlr_judge, then play with candidate = its number (or pattern = its code), before bar {c['choiceBar']}"
                           if c["playing"] else "choose with axlr_judge, then play with candidate = its number: your first pattern starts at the next bar")
        elif full:
            out["note"] = f"your fork opens at bar {c['choiceBar'] - self.lead_bars}: call listen"
        return out

    def _ask(self, chair, c, candidates):
        s = self.sections[c["section"]]
        now = c["playing"]
        lines = [f"A techno set at {self.bpm:g} bpm, live-coded in Strudel: one cycle of a pattern is one bar, a phrase is {self.phrase_bars} bars.",
                 f"You are {chair}, the {s['name'].lower()} ({s['family']}). Bar {self._now_bar()} of take {self.take}; dynamics {c['dynamics']}.",
                 f"Playing now: {now['id']} ({now['name']}): {now['code']}" if now else "Playing now: nothing yet.",
                 f"Your section: {self._section_summary(c['section'])}; the others: " + "; ".join(self._section_summary(sid) for sid in self.sections if sid != c["section"]) + ".",
                 f"The conductor's last cue for you: {self._cue_for(c) or 'none'}.", "Candidates for your next pattern, from the next bar:"]
        for e in candidates:
            lines.append(f"- pattern {e['n']}: {e['name']} (energy {e['energy']} of 3): {e['code']}")
        return {"state": "\n".join(lines),
                "question": f"Which pattern should {chair} play next, so the set builds and the parts fit together?",
                "options": [f"pattern {e['n']}: {e['name']}" for e in candidates]}

    def _section_summary(self, sid):
        chairs = [c for c in self.chairs.values() if c["section"] == sid]
        playing = [c for c in chairs if self._state(c) == PLAYING]
        what = f", playing {playing[0]['playing']['id']}" if len(playing) == 1 and len(chairs) == 1 else ""
        return f"{self.sections[sid]['name']}: {len(playing)} of {len(chairs)} playing{what}"

    def _cue_for(self, c):
        for key in (c["section"], self.sections[c["section"]]["family"], "all"):
            if key in self.cues:
                cue = self.cues[key]
                data = cue["data"]
                extra = ", ".join(f"{k} {v}" for k, v in data.items() if k in ("dynamics", "gain", "bar"))
                return cue["name"] + (f" ({extra})" if extra else "")
        return None

    def _judge_report(self, judge, entry, c):
        """Jev's pick as the bot reported it, and whether the choice names the pattern played."""
        if not isinstance(judge, dict):
            return None
        choice = str(judge.get("choice", ""))[:200]
        head = choice.split(":")[0].strip().lower()
        named = next((e for e in c["candidates"] if f"pattern {e['n']}" == head), None)
        report = {"choice": choice, "pattern": entry["id"], "bar": self._now_bar(), "source": JUDGE_SOURCE,
                  "matchesPattern": None if named is None else named["id"] == entry["id"]}
        for key in ("probability", "confidence"):
            if isinstance(judge.get(key), (int, float)):
                report[key] = round(float(judge[key]), 3)
        return report

    def _pattern_view(self, c):
        def view(plan):
            if plan is None:
                return None
            return {"id": plan["id"], "name": plan["name"], "code": plan["code"], "custom": plan["custom"], "fromBar": plan["fromBar"],
                    "eventsPerBar": plan["perBar"]}
        return {"playing": view(c["playing"]), "next": view(c["next"]), "choiceBar": c["choiceBar"], "source": "what the chair's own play did"}

    def _chair_view(self, chair, c):
        return {"id": chair, "role": c["role"], "state": self._state(c), "dynamics": c["dynamics"], "gain": round(c["gain"], 2),
                "held": c["held"], "enteredBar": c["enteredBar"], "pattern": self._pattern_view(c), "judge": c["judge"], "hits": c["hits"]}

    def _state(self, c):
        if c["stopped"]:
            return STOPPED
        if c["playing"] is not None and self.status == "playing":
            return PLAYING
        if c["next"] is not None:
            return SCHEDULED
        return WAITING

    def _fork_open(self, c):
        if c["playing"] is None:
            return True
        return self._shown_beat() >= (c["choiceBar"] - self.lead_bars - 1) * BEATS_PER_BAR + c["forkBeats"]

    def _chair(self, chair):
        if chair not in self.chairs:
            raise Refused(f"no such chair: {chair}")
        return self.chairs[chair]

    def _level(self, dynamics):
        if dynamics not in self.levels:
            raise Refused(f"dynamics is one of {', '.join(self.levels)}")
        return self.levels[dynamics]

    def _reset_chairs(self, first=False):
        """A new take: every chair silent, waiting, at full gain. The candidates a chair was offered stay its own."""
        for c in self.chairs.values():
            c.update({"playing": None, "next": None, "stopped": False, "dynamics": "mf", "gain": 1.0, "gainTarget": 1.0, "fadeRate": 0.0,
                      "held": None, "enteredBar": None, "choiceBar": 1 + self.phrase_bars, "judge": None, "hits": 0})
            if first:
                c.update({"candidates": [], "asked": 0, "owned": 0})

    def _start_take(self, why):
        self.take += 1
        self.status = "playing"
        self.beat = -self.count_in * BEATS_PER_BAR
        self.cut = None
        self._reset_chairs()
        self.sound.mute(False)
        self.takes[self.take] = {"startedAt": round(self.clock(), 3), "why": why, "countIn": self.count_in, "bpm": self.bpm}
        self.log.append({"what": "take", "at": round(self.clock(), 3), "take": self.take, "why": why})
        self._fact("take", None, take=self.take, why=why, countIn=self.count_in)

    def _count_in_left(self):
        return min(self.count_in, int(math.ceil(-self._shown_beat() / BEATS_PER_BAR)))

    def _shown_beat(self):
        """The beat that sounds now: the scheduled beat, less the lookahead."""
        return self.beat - self.lookahead * self.bpm / 60.0

    def _bar_of(self, beat):
        return 1 if beat < 0 else int(beat // BEATS_PER_BAR) + 1

    def _now_bar(self):
        return self._bar_of(self._shown_beat()) if self.status == "playing" else self._bar_of(self.beat)

    def _next_bar(self):
        """The first bar boundary not yet scheduled: a play now starts there."""
        if self.beat < 0:
            return 1
        return int(self.beat // BEATS_PER_BAR) + 2

    def _seconds_until_bar(self, bar):
        return ((bar - 1) * BEATS_PER_BAR - self._shown_beat()) * 60.0 / self.bpm

    def _fade_to(self, c, gain, seconds):
        c["gainTarget"] = gain
        c["fadeRate"] = abs(gain - c["gain"]) / max(0.01, seconds)

    def _fading(self, seconds):
        moved = False
        for c in self.chairs.values():
            if c["gain"] != c["gainTarget"]:
                step = c["fadeRate"] * seconds
                if abs(c["gainTarget"] - c["gain"]) <= step:
                    c["gain"] = c["gainTarget"]
                else:
                    c["gain"] += step if c["gainTarget"] > c["gain"] else -step
                moved = True
        if moved and self.status != "playing":
            self._changed()

    def _fact(self, what, chair, **data):
        self.facts.append({"at": round(self.clock(), 3), "what": what, "chair": chair, "bar": self._now_bar(), **data})
        del self.facts[:-200]
        self._changed()

    def _changed(self):
        self.version += 1
        self.cond.notify_all()
