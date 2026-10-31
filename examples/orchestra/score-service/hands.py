"""The conductor's hands: postures from the headset, read by Jev, published on the team's bus.

The page in the headset (static/hands.html, WebXR hand input) measures each
hand's 25 joints: how far each finger bends, where the palm faces, where the
hand is. When a posture has held still for a moment it sends it here (POST
/api/hands), and the service asks Jev, one choice per hand: nothing, open
hand, palm down or fist. Nothing is the default: a hand Jev does not read as a
gesture with at least the threshold's probability gives no signal.

A gesture acts on the instrument the conductor looks at: the page keeps the
selection (the one the head points at, locked with a short pinch) and sends
it as target; each instrument has its slice of the hall's arc
(hands.instruments: violins, oboes, horns, violas, cellos, basses). Without a
target, a hand acts on the instrument it points at (aim, degrees: 0 to the
conductor's right, 90 ahead, 180 to the left), and without that on its old
group: the left hand on the strings, the right on the winds. Both hands
showing the same gesture act on everyone:

  palm up     info.cue.<group> {"dynamics": "f"}   come in, or play loud
  palm down   info.cue.<group> {"dynamics": "p"}   come in, or play soft
  fist        fade the group out (both fists: everyone), no event
  finger on the lips   info.cue.all {"dynamics": "p"} and everyone's gain
              to hush.gain: pianissimo
  pointing    a baton hand: no signal by itself; the page measures its beats
  the hand's height, after a palm up or a palm down: a provisional level
              for the group, from where the hand began over height.span
              metres, heard at once; a double pinch keeps it, else it comes
              back where it was when the hand drops, leaves (no height for
              height.silentSeconds), waits height.pendingSeconds unconfirmed
              or Jev reads it as another gesture; no event

The headset's own gestures come as native messages, read by the headset,
not by Jev, and act on the target (native):

  tap-thumb       the entrance of the section the conductor looks at, locked
                  or not: info.cue.<section> with no level, while the music plays
  swipe-forward   the tempo up by tempoStep; swipe-backward: down; the clock
                  at once, info.tempo {"bpm"} published at most every
                  tempoPublishSeconds
  pinch-start / pinch-move / pinch-end   a pinch held 350 ms or more (the
                  page's; shorter is a tap): the target's gain, from where it
                  was when the hold began, by the hand's rise or fall over
                  pinchSpan metres; released, the level stays; no event
  pinch-double    two taps within 350 ms (the page's): keeps the level a flat
                  hand set (either hand); one tap locks or frees the section
  beat            the tempo the conductor beats with a pointing hand, the
                  clock at once, published as the swipes are

The conductor's expression is read from how the hands move, not from what
they hold (motion): every couple of seconds the page sums up each hand's last
seconds (speed, how far it rose and swept, how often it turned, how open, how
high) and the service asks Jev one choice: dolce, cantabile, brillante,
appassionato or nothing, with the scale of this conductor's own motion.
Nothing is the default, as with the gestures: a reading under
expression.threshold changes nothing, and nothing is read while nothing
plays (an event would reach each idle chair as a task, and a Jev chair's
task starts the music). A character read expression.agreeReads times in a
row (2) goes on the chairs of the section looked at, else on everyone, at
once (orchestra.character): their voices darker or brighter (dolce also
struck softer), never their dynamics or gain, which the cues and the hands'
heights own. It is published as info.expression {"character"}
when it changes, at most every expression.publishSeconds, so a chair's bot
hears the conductor and asks for it in its next play; the sound does not
wait for the bus.

A cue is the event the operator's panel already publishes (orchestra 6b), as
the operator; it reaches a bot's running task as a message. A pause is the
service's alone: a stop on the bus would end each bot's run, and a bot back
from a stop has to start a new one (seconds). Faded out, a chair keeps its
phrases and its bot goes on choosing; the cue that names it fades it back in.
The height is read many times a second without Jev: it is a measurement, not
a sign, and it only acts while the hand conducts a level: from Jev's reading
of a palm up or a palm down until Jev reads another gesture (a fist, a
pointing hand, a finger on the lips, or the other level; the same one read
on another section keeps the level where it began). A reading of nothing, or one under the threshold, keeps it:
a flat hand that relaxes is still the conductor's. The page sends the height
while the four fingers are not curled. What the height sets is provisional:
a double pinch keeps it; a hand lowered to rest (a fast drop, a fist, or a
hand gone for height.silentSeconds, 1.5 s) brings the level back where it
was, so resting the arm never turns a section down. A pinch held takes the level
over and keeps it when released; the height then starts again from there.

The service is the orchestra, so it does what a gesture means at once (holds
the group, sets the level, starts the count-in) and publishes the events in
parallel, without waiting for them to come back on its own bus: the bots
hear them, the service does not apply its own echo (bus.Bus.publish). The
page's answer does not wait for the control plane either: each event's id,
or why it failed, is filled in when the publish answers (GET /api/hands).

An event that would change nothing is not sent, and why is kept: a pause when
nothing of the group plays, a cue when every chair it names already plays at
that level, the same gesture again within the cooldown. Everything read and sent is kept for the
page.
"""

import json
import threading
import time

GESTURES = {
    "nothing": "nothing: no signal (a hand at rest, relaxed, half-closed, moving, or none of the others)",
    "open": "palm up: fingers straight, the hand flat, the palm facing the ceiling",
    "down": "palm down: fingers straight, the palm facing the floor",
    "fist": "fist: all four fingers curled tight into the palm",
    "point": "pointing: the index finger straight, the other fingers curled, the hand away from the face (a baton)",
    "hush": "finger on the lips: the index finger straight, the other fingers curled, the hand right in front of the face, "
            "just below the eyes, within 25 cm of the head",
}
LEVELS = {"open": "f", "down": "p"}
CHARACTERS = {
    "nothing": "nothing: no character to read (the hands barely move, or the motion says nothing in particular)",
    "dolce": "dolce: tender and soft; slow, small, smooth motions, the hands low, the fingers relaxed",
    "cantabile": "cantabile: singing and even; moderate, flowing, continuous motion",
    "brillante": "brillante: bright and lively; quick, light motions with small rises, the hands open",
    "appassionato": "appassionato: fiery and intense; fast, big, forceful motions, the hands rising and falling far",
}
MOTION_HEAD = ("A conductor wearing a mixed-reality headset conducts an orchestra with their hands. The headset tracked each hand's "
               "position for the last {seconds} s; these are the sums of that motion, not a single posture. The orchestra: {music}.\n"
               "For scale, from this conductor's own hands: half the time they move under 0.4 m/s and nine times in ten under 1 m/s, "
               "rise and fall over about 25 cm and turn about twice a second. Beating time always gives peaks of 3 to 8 times the "
               "average speed: that is the beat, normal in every character, not an accent; judge by the average speed and the rise. "
               "A tender passage is conducted slowly, under about 0.2 m/s, with small rises; "
               "an even, singing one at a moderate 0.2–0.5 m/s; "
               "a bright, lively one quickly and lightly, about 0.5–0.9 m/s with small rises, under about 40 cm; "
               "a fiery one fast and big, over about 0.9 m/s with rises over about 50 cm. "
               "A hand that barely moves (under 0.05 m/s) says nothing.")
NATIVE = ("tap-thumb", "swipe-forward", "swipe-backward", "pinch-start", "pinch-move", "pinch-end", "pinch-double", "beat")
FINGERS = ("thumb", "index", "middle", "ring", "little")
FACING = (("away", "away from the conductor, toward the orchestra"), ("floor", "the floor"),
          ("toward", "the conductor"), ("up", "the ceiling"), ("side", "sideways"))
JEV_SOURCE = "Jev (TypeSafe), asked by the score service with the hand measurements as text; no image leaves the headset"
STATE_HEAD = ("A conductor wearing a mixed-reality headset signals an orchestra with their hands. The headset tracks each "
              "hand's joints; these measurements are from one moment, the hand held still for {held} ms.\n"
              "Finger bend is the sum of the angles at a finger's joints: about 0–40° is straight, 60–120° half bent, "
              "150° and more curled into the palm. Palm facing lists the cosine between the palm's normal and each "
              "direction: the largest is where the palm faces.")


class Hands:
    def __init__(self, orchestra, config, judge, publish=None, clock=time.time, log=None, watch=None):
        self.orchestra, self.judge, self.publish, self.clock = orchestra, judge, publish, clock
        self.log_path = log  # one JSON line per record and per level kept or reverted: the session, for reading afterwards
        self.threshold = config.get("threshold", 0.8)
        self.cooldown = config.get("cooldownSeconds", 3.0)
        self.min_interval = config.get("minIntervalSeconds", 0.5)
        self.groups = config.get("groups", {"left": "strings", "right": "winds", "both": "all"})
        self.dry_run = bool(config.get("dryRun")) or publish is None
        self.publish_wait = config.get("publishWaitSeconds", 0)
        height = config.get("height", {})
        self.height_span, self.height_fade = float(height.get("span", 0.7)), float(height.get("fadeSeconds", 0.2))
        self.drop_speed = float(height.get("dropSpeed", 0.8))  # m/s downward: the hand is dropping, not conducting
        self.pending_seconds = float(height.get("pendingSeconds", 5.0))  # a level held still this long unconfirmed comes back
        self.silent_seconds = float(height.get("silentSeconds", 1.5))  # no height for this long: the hand left
        self.engaged = {"left": None, "right": None}  # the flat hand that conducts a group: gesture, group, the height and gain it began from
        self.pending = {"left": None, "right": None}  # a provisional level: group, gain, the level it came from, when it moved, when last heard
        self.confirmed = {}  # each group's last level kept, for the page
        self.last_height = {"left": None, "right": None}
        self.last_gain = None  # the last level kept or reverted, for the page
        self.instruments = {i["id"]: i for i in config.get("instruments", [])}
        self.tempo_step = float(config.get("tempoStep", 4))
        self.tempo_every = float(config.get("tempoPublishSeconds", 3.0))
        self.pinch_span = float(config.get("pinchSpan", 0.35))
        self.hush_gain = float((config.get("hush") or {}).get("gain", 0.55))
        self.native_cooldown = float(config.get("nativeCooldownSeconds", 1.0))
        self.pinch = {"left": None, "right": None}  # where a held pinch began: the hand's height, the target's gain
        self.tempo_sent_at = None
        expression = config.get("expression") or {}
        self.character_threshold = float(expression.get("threshold", 0.55))
        self.motion_every = float(expression.get("everySeconds", 2.0))
        self.character_every = float(expression.get("publishSeconds", 6.0))
        self.agree_reads = max(1, int(expression.get("agreeReads", 2)))  # a character is applied once this many readings in a row agree
        self.streak = (None, 0)  # the last character read and how many readings in a row read it
        self.motion_at, self.character_sent_at, self.character = None, None, None  # the last character read, for the page
        self.lock = threading.Lock()
        self.last_call, self.sent_at, self.history, self.calls = None, {}, [], 0
        if (watch if watch is not None else clock is time.time):  # the wall clock: a thread reverts what waits too long; tests tick by hand
            threading.Thread(target=self._watch, daemon=True).start()

    # --- the page's posture ---------------------------------------------------

    def observe(self, posture):
        """One posture held still: Jev's reading, what it means and what was published. Returns the record."""
        hands = {side: h for side, h in ((posture or {}).get("hands") or {}).items() if side in ("left", "right") and isinstance(h, dict)}
        held = int((posture or {}).get("heldMs") or 0)
        target = self.target_of((posture or {}).get("target"))
        now = self.clock()
        record = {"at": round(now, 3), "heldMs": held, "hands": {side: self._compact(h) for side, h in hands.items()},
                  "target": target, "locked": bool((posture or {}).get("locked")),
                  "read": {}, "decision": [], "sent": [], "notSent": [], "dryRun": self.dry_run,
                  "simulated": bool((posture or {}).get("simulated"))}
        with self.lock:
            if not hands:
                record["notSent"].append({"what": "nothing", "why": "no hand tracked"})
                return self._keep(record)
            if self.last_call is not None and now - self.last_call < self.min_interval:
                record["notSent"].append({"what": "nothing", "why": f"too soon after the last reading ({self.min_interval} s apart at most)"})
                return self._keep(record)
            self.last_call = now
            self.calls += 1
        options = list(GESTURES.values())
        questions = {side: (f"Which signal does the conductor's {side} hand give?", options) for side in sorted(hands)}
        try:
            answer = self.judge(self.state(hands, held), questions)
        except Exception as exc:  # Jev failing means no signal, never a guess
            record["jev"] = {"error": str(exc), "source": JEV_SOURCE}
            record["notSent"].append({"what": "nothing", "why": "Jev did not answer"})
            with self.lock:
                return self._keep(record)
        by_text = {v: k for k, v in GESTURES.items()}
        record["jev"] = {"model": answer["model"], "ms": answer["ms"], "inputTokens": answer.get("inputTokens"), "source": JEV_SOURCE,
                         "answers": {side: {"choice": by_text[a["choice"]], "confidence": a.get("confidence"),
                                            "probabilities": {by_text[o]: round(p, 3) for o, p in a["probabilities"].items()}}
                                     for side, a in answer["answers"].items()}}
        for side, a in record["jev"]["answers"].items():
            p = a["probabilities"][a["choice"]]
            record["read"][side] = a["choice"] if a["choice"] != "nothing" and p >= self.threshold else "nothing"
        aims = {side: self.aimed(h.get("aim")) for side, h in hands.items()}
        record["aim"] = {side: group for side, group in aims.items() if group}
        threads = []
        with self.lock:
            meant = self.meaning(record["read"], aims, target)
            for side in record["read"]:  # a flat hand conducts the group its gesture went to, from where it is now
                g, e = record["read"][side], self.engaged[side]
                if g == "nothing":
                    continue  # a hand that relaxes, or that Jev is unsure of, goes on conducting its level
                if g in LEVELS:
                    group = next(grp for gg, grp in meant if gg == g)
                    if e and e["gesture"] == g:
                        continue  # a glance at another section: the level in progress stays with the one it began on
                    self._disengage(side, now)  # the other level gesture: what the hand held comes back
                    self.engaged[side] = {"gesture": g, "group": group, "height": None, "gain": None}
                else:
                    self._disengage(side, now)  # a hand that closes, points or hushes conducts no level: what it held comes back
            for gesture, group in meant:
                record["decision"].append({"gesture": gesture, "group": group})
                threads += self._act(gesture, group, record)
            self._keep(record)
        for thread in threads if self.publish_wait > 0 else ():  # the sound changed already; the ids come when the publish answers
            thread.join(timeout=self.publish_wait)
        return record

    def height(self, message):
        """Each hand's height, many times a second, while it holds a palm up or a palm down: a provisional level for its
        group, from where the hand began over height.span metres, heard at once. A double pinch keeps it; else it comes
        back where it was when the hand drops (dropSpeed), leaves (silentSeconds) or waits unconfirmed (pendingSeconds).
        A height marked pinch only says the hand is still there. Returns each hand's provisional gain."""
        now = self.clock()
        gains = {}
        with self.lock:
            for side, h in ((message or {}).get("hands") or {}).items():
                if side not in self.engaged or not isinstance(h, dict) or not isinstance(h.get("height"), (int, float)):
                    continue
                height, e, p = float(h["height"]), self.engaged[side], self.pending[side]
                if h.get("pinch"):
                    if p:
                        p["at"] = now
                    continue
                if not e or self.pinch[side]:
                    continue
                before, self.last_height[side] = self.last_height[side], (height, now)
                if before and now > before[1] and (before[0] - height) / (now - before[1]) > self.drop_speed:
                    self._revert(side, "the hand dropped", now)
                    continue
                if e["height"] is None:
                    e["height"], e["gain"] = height, self._gain_of(e["group"])
                gain = round(max(0.0, min(1.0, e["gain"] + (height - e["height"]) / self.height_span)), 4)
                if p is None:
                    p = self.pending[side] = {"group": e["group"], "gain": gain, "from": e["gain"], "since": now, "moved": now, "at": now}
                    self._log({"at": round(now, 3), "gain": {"kind": "pending", "side": side, "group": e["group"], "from": round(e["gain"], 2)}})
                else:
                    if abs(gain - p["gain"]) >= 0.02:
                        p["moved"] = now
                    p["gain"], p["at"] = gain, now
                self.orchestra.fade(self.chairs_of(e["group"]), gain, self.height_fade)
                gains[side] = round(gain, 2)
        return gains

    def tick(self, now=None):
        """Four times a second: a provisional level whose hand left (no height for silentSeconds) or that waited
        unconfirmed for pendingSeconds comes back where it was. Returns the records of what came back."""
        now = self.clock() if now is None else now
        done = []
        with self.lock:
            for side, p in list(self.pending.items()):
                if not p:
                    continue
                if now - p["at"] > self.silent_seconds:
                    done.append(self._revert(side, "the hand left", now))
                elif now - p["moved"] > self.pending_seconds:
                    done.append(self._revert(side, f"not kept within {self.pending_seconds:g} s", now))
        return done

    def _watch(self):
        while True:
            time.sleep(0.25)
            try:
                self.tick()
            except Exception:  # noqa: BLE001  a watchdog never dies
                pass

    def _gain_of(self, group):
        chairs = {c["id"]: c for s in self.orchestra.snapshot()["sections"] for c in s["chairs"]}
        named = self.chairs_of(group)
        return sum(chairs[k].get("gain", 1.0) for k in named) / len(named) if named else 1.0

    def _title(self, group):
        return self.instruments[group]["name"] if group in self.instruments else "Everyone" if group == "all" else group.capitalize()

    def _confirm(self, side, now):
        """The level a flat hand set stays: the double pinch. Under the lock."""
        p, self.pending[side], self.engaged[side], self.last_height[side] = self.pending[side], None, None, None
        self.confirmed[p["group"]] = p["gain"]
        self.orchestra.fade(self.chairs_of(p["group"]), p["gain"], 0)
        self.last_gain = {"kind": "kept", "side": side, "group": p["group"], "gain": round(p["gain"], 2), "from": round(p["from"], 2), "at": round(now, 3)}
        self._keep({"at": round(now, 3), "gain": self.last_gain})
        return self.last_gain

    def _revert(self, side, why, now):
        """A provisional level comes back where it was; the hand conducts no level until Jev reads it again. Under the lock."""
        p, self.pending[side], self.engaged[side], self.last_height[side] = self.pending[side], None, None, None
        self.orchestra.fade(self.chairs_of(p["group"]), p["from"], self.height_fade)
        self.last_gain = {"kind": "reverted", "side": side, "group": p["group"], "gain": round(p["from"], 2), "from": round(p["gain"], 2),
                          "why": why, "at": round(now, 3)}
        self._keep({"at": round(now, 3), "gain": self.last_gain})
        return self.last_gain

    def _disengage(self, side, now):
        if self.pending[side]:
            self._revert(side, "the hand changed", now)
        self.engaged[side], self.last_height[side] = None, None

    def aimed(self, aim):
        """The instrument whose slice of the arc an aim (degrees) falls in, or None."""
        if not isinstance(aim, (int, float)):
            return None
        return next((i for i, x in self.instruments.items() if x["from"] <= aim < x["to"]), None)

    def target_of(self, target):
        """The instrument the page says the conductor looks at, if it is one."""
        return target if isinstance(target, str) and target in self.instruments else None

    def chairs_of(self, group):
        if group in self.instruments:
            return [k for sid in self.instruments[group]["sections"] for k in self.orchestra.chairs_of(sid)]
        return self.orchestra.chairs_of(group)

    def meaning(self, read, aims=None, target=None):
        """Each hand's gesture on the target (else the instrument it points at, else its old group); both hands the same gesture: everyone.

        A finger on the lips is for everyone whichever hand; a pointing hand is no signal."""
        gestures = {side: g for side, g in read.items() if g not in ("nothing", "point")}
        if "hush" in gestures.values():
            return [("hush", self.groups["both"])]
        if len(gestures) == 2 and gestures["left"] == gestures["right"]:
            return [(gestures["left"], self.groups["both"])]
        meant = []
        for side, g in sorted(gestures.items()):
            group = target or (aims or {}).get(side) or self.groups[side]
            if (g, group) not in meant:  # two hands on one instrument: one gesture
                meant.append((g, group))
        return meant

    # --- the headset's own gestures -------------------------------------------

    def native(self, message):
        """One gesture the headset read itself (tap-thumb, swipes, a pinch held, a beat): what it means on the target, done and sent. Returns the record."""
        message = message if isinstance(message, dict) else {}
        gesture, side = message.get("gesture"), message.get("side") if message.get("side") in ("left", "right") else None
        target, locked = self.target_of(message.get("target")), bool(message.get("locked"))
        now = self.clock()
        record = {"at": round(now, 3), "native": gesture, "side": side, "target": target, "locked": locked,
                  "read": {}, "decision": [], "sent": [], "notSent": [], "dryRun": self.dry_run}
        threads = []
        with self.lock:
            if gesture not in NATIVE:
                record["notSent"].append({"what": str(gesture), "why": "not a gesture the headset gives"})
            elif gesture == "tap-thumb":
                threads = self._entrance(target, locked, record)
            elif gesture in ("swipe-forward", "swipe-backward"):
                bpm = self.orchestra.set_tempo(self.orchestra.bpm + (self.tempo_step if gesture == "swipe-forward" else -self.tempo_step))
                record["applied"] = f"tempo {round(bpm)}"
                threads = self._tempo(bpm, record)
            elif gesture == "beat":
                bpm = message.get("bpm")
                if isinstance(bpm, (int, float)) and bpm > 0:
                    bpm = self.orchestra.set_tempo(float(bpm))
                    record["applied"] = f"tempo {round(bpm)}, as beaten"
                    threads = self._tempo(bpm, record)
                else:
                    record["notSent"].append({"what": "beat", "why": "no tempo in the beat"})
            elif gesture == "pinch-start":
                if side:  # the pinch takes the level over from the flat hand: no jump, nothing comes back
                    self.pending[side], self.engaged[side], self.last_height[side] = None, None, None
                self._pinch_start(side, target, message.get("height"), record)
            elif gesture == "pinch-move":
                self._pinch_move(side, message.get("height"), record)
            elif gesture == "pinch-end":
                p = self.pinch.get(side) if side else None
                if side:
                    self.pinch[side] = None
                if p:  # released: the level stays, kept
                    gain = round(self._gain_of(p["target"]), 4)
                    self.confirmed[p["target"]] = gain
                    self.last_gain = {"kind": "kept", "side": side, "group": p["target"], "gain": round(gain, 2), "from": round(p["gain"], 2), "at": round(now, 3)}
                    record["gain"] = self.last_gain
                    record["applied"] = f"{self._title(p['target'])} kept at {round(100 * gain)}%"
                else:
                    record["applied"] = "pinch released"
            elif gesture == "pinch-double":
                kept = [self._confirm(s, now) for s in ("left", "right") if self.pending[s]]
                if kept:
                    record["decision"] += [{"gesture": "pinch-double", "group": k["group"]} for k in kept]
                    record["applied"] = "; ".join(f"{self._title(k['group'])} kept at {round(100 * k['gain'])}%" for k in kept)
                else:
                    record["notSent"].append({"what": "keep", "why": "no level waiting: raise or lower a flat hand first"})
            self._keep(record)
        for thread in threads if self.publish_wait > 0 else ():
            thread.join(timeout=self.publish_wait)
        return record

    # --- the conductor's expression, from how the hands move -------------------

    def motion(self, message):
        """The hands' last seconds, summed up by the page: Jev reads the expression; a character read expression.agreeReads times in
        a row goes on the section looked at, else everyone."""
        message = message if isinstance(message, dict) else {}
        hands = {side: h for side, h in (message.get("hands") or {}).items() if side in ("left", "right") and isinstance(h, dict)}
        target, locked = self.target_of(message.get("target")), bool(message.get("locked"))
        now = self.clock()
        record = {"at": round(now, 3), "motion": {side: self._compact_motion(h) for side, h in hands.items()}, "target": target, "locked": locked,
                  "read": {}, "decision": [], "sent": [], "notSent": [], "dryRun": self.dry_run, "simulated": bool(message.get("simulated"))}
        with self.lock:
            if not hands:
                record["notSent"].append({"what": "expression", "why": "no hand tracked"})
                return self._keep(record)
            if self.motion_at is not None and now - self.motion_at < self.motion_every:
                record["notSent"].append({"what": "expression", "why": f"too soon after the last reading ({self.motion_every:g} s apart at most)"})
                return self._keep(record)
            self.motion_at = now
            self.calls += 1
        options = list(CHARACTERS.values())
        try:
            answer = self.judge(self.motion_state(hands), {"expression": ("Which expression does the conductor ask of the orchestra?", options)})
        except Exception as exc:  # Jev failing means no change, never a guess
            record["jev"] = {"error": str(exc), "source": JEV_SOURCE}
            record["notSent"].append({"what": "expression", "why": "Jev did not answer"})
            with self.lock:
                return self._keep(record)
        by_text = {v: k for k, v in CHARACTERS.items()}
        a = answer["answers"]["expression"]
        probabilities = {by_text[o]: round(p, 3) for o, p in a["probabilities"].items()}
        record["jev"] = {"model": answer["model"], "ms": answer["ms"], "inputTokens": answer.get("inputTokens"), "source": JEV_SOURCE,
                         "answers": {"expression": {"choice": by_text[a["choice"]], "confidence": a.get("confidence"), "probabilities": probabilities}}}
        choice, p = by_text[a["choice"]], probabilities[by_text[a["choice"]]]
        read = choice if choice != "nothing" and p >= self.character_threshold else "nothing"
        record["read"] = {"expression": read, "probability": p}
        threads = []
        with self.lock:
            if read == "nothing":
                self.streak = (None, 0)
                record["notSent"].append({"what": "expression", "why": f"Jev read {choice} at {p:.2f}" + ("" if choice == "nothing" else f", under {self.character_threshold:g}")})
                return self._keep(record)
            # Only while the music plays: a message reaches an idle chair as a task, and a Jev chair's task
            # plays its first phrase, which starts the music. Hands warming up before the downbeat cue nobody.
            if self.orchestra.snapshot()["status"] != "playing":
                self.streak = (None, 0)
                record["notSent"].append({"what": f"{read}", "why": "nothing plays: a palm up starts the music"})
                return self._keep(record)
            # One reading is not enough: the conductor's hands read differently from one beat to the next.
            self.streak = (read, self.streak[1] + 1 if self.streak[0] == read else 1)
            if self.streak[1] < self.agree_reads:
                record["notSent"].append({"what": f"{read}", "why": f"read {read} {self.streak[1]} of {self.agree_reads} times in a row: waits for the next reading to agree"})
                return self._keep(record)
            group = target or "all"
            record["decision"].append({"gesture": read, "group": group})
            named = self.chairs_of(group)
            changed = self.orchestra.character(named, read)
            self.character = {"character": read, "probability": p, "group": group, "at": round(now, 3)}
            title = self.instruments[group]["name"] if group in self.instruments else "Everyone"
            record["applied"] = f"{title}: {read}" + ("" if changed else " already")
            if not changed:
                record["notSent"].append({"what": f"{read} {group}", "why": f"the group already plays {read}"})
            elif self.character_sent_at is not None and now - self.character_sent_at < self.character_every:
                record["notSent"].append({"what": f"{read} {group}", "why": f"the expression was published {now - self.character_sent_at:.1f} s ago; the voices follow already"})
            else:
                self.character_sent_at = now
                threads = self._send([("info.expression", f"{title}, {read}.", {"character": read, "target": group}, None)], record)
            self._keep(record)
        for thread in threads if self.publish_wait > 0 else ():
            thread.join(timeout=self.publish_wait)
        return record

    def motion_state(self, hands):
        """The motion sums as Jev reads them: one sentence per hand tracked, and where the music is."""
        snap = self.orchestra.snapshot()
        music = (f"playing bar {snap['bar']} of {snap['bars']} at {round(snap['bpm'])} bpm, {sum(s['playing'] for s in snap['sections'])} chairs sounding"
                 if snap["status"] == "playing" else "nothing plays yet")
        seconds = max((float(h.get("seconds") or 0) for h in hands.values()), default=0)
        lines = [MOTION_HEAD.format(seconds=f"{seconds:.1f}", music=music)]
        for side in ("left", "right"):
            h = hands.get(side)
            if h is None:
                lines.append(f"{side.capitalize()} hand: not tracked.")
                continue
            num = lambda k: float(h[k]) if isinstance(h.get(k), (int, float)) else None  # noqa: E731
            speed, peak, rise, sweep, turns, open_, height = (num(k) for k in ("speed", "peak", "rise", "sweep", "turns", "open", "height"))
            text = [f"{side.capitalize()} hand:"]
            if speed is not None:
                text.append(f"moved at {speed:.2f} m/s on average" + (f", peaks of {peak:.2f} m/s ({peak / speed:.1f} times the average)" if peak and speed > 0.01 else "") + ";")
            if rise is not None and sweep is not None:
                text.append(f"rose and fell over {rise * 100:.0f} cm and swept {sweep * 100:.0f} cm sideways;")
            if turns is not None:
                text.append(f"turned from rising to falling {turns:.1f} times a second;")
            if height is not None:
                text.append(f"was {abs(height) * 100:.0f} cm {'above' if height >= 0 else 'below'} the eyes on average;")
            if open_ is not None:
                text.append(f"fingers {open_:.2f} open (1 straight, 0 curled into the palm).")
            lines.append(" ".join(text))
        return "\n".join(lines)

    def _compact_motion(self, h):
        return {k: round(float(h[k]), 2) for k in ("seconds", "speed", "peak", "rise", "sweep", "turns", "open", "height") if isinstance(h.get(k), (int, float))}

    def _entrance(self, target, locked, record):
        """A thumb tap: the entrance of the section the conductor looks at (locked or not), a cue with no level, while the music plays."""
        o = self.orchestra
        if o.snapshot()["status"] != "playing":
            record["notSent"].append({"what": "entrance", "why": "nothing plays: a palm up starts the music"})
            return []
        if not target:
            record["notSent"].append({"what": "entrance", "why": "no section: look at one"})
            return []
        now = self.clock()
        last = self.sent_at.get(("entrance", target))
        if last is not None and now - last < self.native_cooldown:
            record["notSent"].append({"what": f"entrance {target}", "why": f"the same tap was sent {now - last:.1f} s ago"})
            return []
        self.sent_at[("entrance", target)] = now
        name = self.instruments[target]["name"]
        events = [(f"info.cue.{sid}", f"{name}, come in.", None, None) for sid in self.instruments[target]["sections"]]
        applied = [o.cue(ev, {}) for ev, _, _, _ in events]
        record["decision"].append({"gesture": "tap-thumb", "group": target})
        record["applied"] = "; ".join(a for a in applied if a) or f"{name}: entrance"
        return self._send(events, record)

    def _tempo(self, bpm, record):
        """The clock has the tempo already; the bus hears it at most every tempoPublishSeconds."""
        now = self.clock()
        if self.tempo_sent_at is not None and now - self.tempo_sent_at < self.tempo_every:
            record["notSent"].append({"what": f"tempo {round(bpm)}", "why": f"the tempo was published {now - self.tempo_sent_at:.1f} s ago; the clock follows already"})
            return []
        self.tempo_sent_at = now
        return self._send([("info.tempo", f"Tempo {round(bpm)}.", {"bpm": bpm}, None)], record)

    def _pinch_start(self, side, target, height, record):
        if not side or not target or not isinstance(height, (int, float)):
            record["notSent"].append({"what": "pinch", "why": "no section to hold" if side and not target else "no hand or height in the pinch"})
            return
        gain = self._gain_of(target)
        self.pinch[side] = {"target": target, "height": float(height), "gain": gain}
        record["applied"] = f"holding {self.instruments[target]['name']} at {round(100 * gain)}%"

    def _pinch_move(self, side, height, record):
        p = self.pinch.get(side) if side else None
        if not p or not isinstance(height, (int, float)):
            record["notSent"].append({"what": "pinch", "why": "no pinch began"})
            return
        gain = round(max(0.0, min(1.0, p["gain"] + (float(height) - p["height"]) / self.pinch_span)), 4)
        self.orchestra.fade(self.chairs_of(p["target"]), gain, self.height_fade)
        record["decision"].append({"gesture": "pinch", "group": p["target"]})
        record["applied"] = f"{self.instruments[p['target']]['name']} {round(100 * gain)}%"

    def _send(self, events, record):
        """Each event kept for the page and published in its own thread, unless this is a dry run."""
        threads = []
        for name, message, data, to in events:
            entry = {"name": name, "message": message, "data": data, "to": to, "eventId": None}
            record["sent"].append(entry)
            if self.dry_run:
                entry["note"] = "dry run: not published"
                continue
            thread = threading.Thread(target=self._publish, args=(entry,), daemon=True)
            thread.start()
            threads.append(thread)
        return threads

    def state(self, hands, held):
        """The measurements as Jev reads them: one paragraph per hand tracked."""
        lines = [STATE_HEAD.format(held=held)]
        for side in ("left", "right"):
            h = hands.get(side)
            if h is None:
                lines.append(f"{side.capitalize()} hand: not tracked.")
                continue
            fingers = h.get("fingers") or {}
            bends = ", ".join(f"{f} {round(float(fingers[f]))}°" for f in FINGERS if isinstance(fingers.get(f), (int, float)))
            palm = h.get("palm") or {}
            facing = sorted(((float(palm[k]), words) for k, words in FACING if isinstance(palm.get(k), (int, float))), reverse=True)
            where = h.get("position") or {}
            text = [f"{side.capitalize()} hand:"]
            if bends:
                text.append(f"finger bend: {bends}.")
            if isinstance(h.get("spread"), (int, float)):
                text.append(f"index to little fingertip: {h['spread'] * 100:.0f} cm.")
            if facing:
                text.append("palm facing: " + ", ".join(f"{words} {value:+.2f}" for value, words in facing) + ".")
            if all(isinstance(where.get(k), (int, float)) for k in ("side", "height", "ahead")):
                text.append(f"position: {abs(where['side']) * 100:.0f} cm to the {'right' if where['side'] >= 0 else 'left'} of the head, "
                            f"{abs(where['height']) * 100:.0f} cm {'above' if where['height'] >= 0 else 'below'} the eyes, "
                            f"{where['ahead'] * 100:.0f} cm in front.")
            lines.append(" ".join(text))
        return "\n".join(lines)

    def view(self):
        with self.lock:
            return {"last": self.history[-1] if self.history else None, "history": self.history[-12:], "calls": self.calls,
                    "threshold": self.threshold, "cooldownSeconds": self.cooldown, "groups": self.groups,
                    "gestures": {"open": "info.cue.<group> f", "down": "info.cue.<group> p", "fist": "fade the group out (the service's, no event)",
                                 "hush": f"info.cue.all p and everyone's gain to {self.hush_gain}", "point": "a baton: no signal, its beats are the tempo",
                                 "height": f"a provisional level for the group after a palm up or down, from where the hand began over {self.height_span:g} m; "
                                           f"kept by a double pinch, else back where it was when the hand drops, leaves for {self.silent_seconds:g} s, "
                                           f"waits {self.pending_seconds:g} s or Jev reads another gesture (the service's, no event)",
                                 "pinch twice": "keeps the level a flat hand set",
                                 "tap-thumb": "the entrance of the section looked at: info.cue.<section>, no level, while the music plays",
                                 "swipe-forward / swipe-backward": f"tempo ±{self.tempo_step:g}, info.tempo at most every {self.tempo_every:g} s",
                                 "pinch held": f"350 ms or more: the target's gain from where it was, over {self.pinch_span:g} m, kept when released, no event",
                                 "beat": "the tempo as beaten with a pointing hand, published as the swipes are",
                                 "motion": f"how the hands move, read by Jev as the expression (dolce, cantabile, brillante, appassionato) of the section looked at, else everyone: "
                                           f"read {self.agree_reads} times in a row: the voices' timbre at once, info.expression when it changes, at most every {self.character_every:g} s"},
                    "character": self.character, "characters": CHARACTERS,
                    "engaged": dict(self.engaged), "pending": {k: v for k, v in self.pending.items() if v}, "confirmed": dict(self.confirmed),
                    "lastGain": self.last_gain, "instruments": list(self.instruments.values()),
                    "dryRun": self.dry_run, "source": JEV_SOURCE}

    # --- inside -------------------------------------------------------------

    def _act(self, gesture, group, record):
        now, o = self.clock(), self.orchestra
        last = self.sent_at.get((gesture, group))
        if last is not None and now - last < self.cooldown:
            record["notSent"].append({"what": f"{gesture} {group}", "why": f"the same gesture was sent {now - last:.1f} s ago"})
            return []
        snap = o.snapshot()
        chairs = {c["id"]: c for s in snap["sections"] for c in s["chairs"]}
        named = self.chairs_of(group)
        if gesture == "fist":
            sounding = [k for k in named if chairs[k]["state"] in ("playing", "scheduled")]
            if snap["status"] != "playing" or not sounding:
                record["notSent"].append({"what": f"fade out {group}", "why": "nothing of the group plays"})
                return []
            events = []  # the bots keep their runs: no stop reaches them
        elif gesture == "hush":
            if snap["status"] != "playing":
                record["notSent"].append({"what": "hush", "why": "nothing plays"})
                return []
            events = [("info.cue.all", "Everyone, pianissimo.", {"dynamics": "p"}, None)]
        else:
            level = LEVELS[gesture]
            if snap["status"] == "playing" and all(chairs[k]["dynamics"] == level and not chairs[k]["held"] for k in named) \
                    and any(chairs[k]["state"] in ("playing", "scheduled") for k in named):
                record["notSent"].append({"what": f"{level} {group}", "why": f"the group already plays {level}"})
                return []
            loud = "loud" if level == "f" else "soft"
            # an instrument is cued section by section: the bots hear info.cue.<section>
            cues = [f"info.cue.{sid}" for sid in self.instruments[group]["sections"]] if group in self.instruments else [f"info.cue.{group}"]
            title = self.instruments[group]["name"] if group in self.instruments else "Everyone" if group == "all" else group.capitalize()
            events = [(name, f"{title}, play {loud}.", {"dynamics": level}, None) for name in cues]
        self.sent_at[(gesture, group)] = now
        why = f"{gesture} from the conductor's hands"
        if gesture == "fist":
            done = o.hold(named, why)
            record["applied"] = f"fading out {len(done['held'])} chairs" if done else "nothing plays"
        elif gesture == "hush":
            o.cue("info.cue.all", {"dynamics": "p"})
            o.fade(named, self.hush_gain, 1.0)
            record["applied"] = f"everyone p at {round(100 * self.hush_gain)}%"
        else:
            applied = [o.cue(name, {"dynamics": LEVELS[gesture]}) for name, _, _, _ in events]
            record["applied"] = "; ".join(a for a in applied if a) or "cue kept"
        return self._send(events, record)

    def _publish(self, entry):
        started = self.clock()
        try:
            entry["eventId"] = self.publish(entry["name"], entry["message"], entry["data"], entry["to"])
        except Exception as exc:
            entry["error"] = str(exc)
        entry["publishMs"] = round((self.clock() - started) * 1000)

    def _compact(self, h):
        fingers = h.get("fingers") or {}
        palm = h.get("palm") or {}
        return {"fingers": {f: round(float(fingers[f])) for f in FINGERS if isinstance(fingers.get(f), (int, float))},
                "palm": {k: round(float(palm[k]), 2) for k, _ in FACING if isinstance(palm.get(k), (int, float))}}

    def _keep(self, record):
        self.history.append(record)
        del self.history[:-200]
        self._log(record)
        return record

    def _log(self, record):
        if not self.log_path:
            return
        try:
            with open(self.log_path, "a") as f:
                f.write(json.dumps(record, default=str) + "\n")
        except OSError:
            pass
