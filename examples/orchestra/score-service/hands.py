"""The conductor's hands: postures from the headset, read by Jev, published on the team's bus.

The page in the headset (static/hands.html, WebXR hand input) measures each
hand's 25 joints: how far each finger bends, where the palm faces, where the
hand is. When a posture has held still for a moment it sends it here (POST
/api/hands), and the service asks Jev, one choice per hand: nothing, open
hand, palm down or fist. Nothing is the default: a hand Jev does not read as a
gesture with at least the threshold's probability gives no signal.

A gesture acts on the instrument the hand points at: the page measures where
the hand is from the eyes on the hall's floor plan (aim, degrees: 0 to the
conductor's right, 90 ahead, 180 to the left), and each instrument has its
slice of the arc (hands.instruments: violins, oboes, horns, violas, cellos,
basses). A hand that gives no aim acts on its old group: the left hand on the
strings, the right on the winds. Both hands showing the same gesture act on
everyone:

  palm up     info.cue.<group> {"dynamics": "f"}   come in, or play loud
  palm down   info.cue.<group> {"dynamics": "p"}   come in, or play soft
  fist        fade the group out (both fists: everyone), no event
  the hand's height, while it holds a palm up or a palm down: the group's
              gain, higher louder, lower softer, a fade, no event

A cue is the event the operator's panel already publishes (orchestra 6b), as
the operator; it reaches a bot's running task as a message. A pause is the
service's alone: a stop on the bus would end each bot's run, and a bot back
from a stop has to start a new one (seconds). Faded out, a chair keeps its
phrases and its bot goes on choosing; the cue that names it fades it back in.
The height is read many times a second without Jev: it is a measurement, not
a sign, and it only acts while Jev's last reading of that hand is a palm up or
a palm down, so a hand at rest or a fist moves nothing. Both are open hands
held flat: the conductor raises and lowers them for the fades.

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

import threading
import time

GESTURES = {
    "nothing": "nothing: no signal (a hand at rest, relaxed, half-closed, moving, or none of the others)",
    "open": "palm up: fingers straight, the hand flat, the palm facing the ceiling",
    "down": "palm down: fingers straight, the palm facing the floor",
    "fist": "fist: all four fingers curled tight into the palm",
}
LEVELS = {"open": "f", "down": "p"}
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
    def __init__(self, orchestra, config, judge, publish=None, clock=time.time):
        self.orchestra, self.judge, self.publish, self.clock = orchestra, judge, publish, clock
        self.threshold = config.get("threshold", 0.8)
        self.cooldown = config.get("cooldownSeconds", 3.0)
        self.min_interval = config.get("minIntervalSeconds", 0.5)
        self.groups = config.get("groups", {"left": "strings", "right": "winds", "both": "all"})
        self.dry_run = bool(config.get("dryRun")) or publish is None
        self.publish_wait = config.get("publishWaitSeconds", 0)
        height = config.get("height", {})
        self.low, self.high, self.height_fade = height.get("low", -0.55), height.get("high", 0.15), height.get("fadeSeconds", 0.2)
        self.engaged = {"left": None, "right": None}  # the gesture and the group a flat hand conducts
        self.instruments = {i["id"]: i for i in config.get("instruments", [])}
        self.lock = threading.Lock()
        self.last_call, self.sent_at, self.history, self.calls = None, {}, [], 0

    # --- the page's posture ---------------------------------------------------

    def observe(self, posture):
        """One posture held still: Jev's reading, what it means and what was published. Returns the record."""
        hands = {side: h for side, h in ((posture or {}).get("hands") or {}).items() if side in ("left", "right") and isinstance(h, dict)}
        held = int((posture or {}).get("heldMs") or 0)
        now = self.clock()
        record = {"at": round(now, 3), "heldMs": held, "hands": {side: self._compact(h) for side, h in hands.items()},
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
            meant = self.meaning(record["read"], aims)
            for side in record["read"]:  # a flat hand conducts the group its gesture went to
                g = record["read"][side]
                self.engaged[side] = (g, next(grp for gg, grp in meant if gg == g)) if g in LEVELS else None
            for gesture, group in meant:
                record["decision"].append({"gesture": gesture, "group": group})
                threads += self._act(gesture, group, record)
            self._keep(record)
        for thread in threads if self.publish_wait > 0 else ():  # the sound changed already; the ids come when the publish answers
            thread.join(timeout=self.publish_wait)
        return record

    def height(self, message):
        """Each hand's height, many times a second: the gain of its group while the hand holds a palm up or a palm down."""
        gains = {}
        for side, h in ((message or {}).get("hands") or {}).items():
            height = (h or {}).get("height") if isinstance(h, dict) else None
            if side not in self.engaged or not self.engaged[side] or not isinstance(height, (int, float)):
                continue
            gain = max(0.0, min(1.0, (height - self.low) / (self.high - self.low)))
            self.orchestra.fade(self.chairs_of(self.engaged[side][1]), gain, self.height_fade)
            gains[side] = round(gain, 2)
        return gains

    def aimed(self, aim):
        """The instrument whose slice of the arc an aim (degrees) falls in, or None."""
        if not isinstance(aim, (int, float)):
            return None
        return next((i for i, x in self.instruments.items() if x["from"] <= aim < x["to"]), None)

    def chairs_of(self, group):
        if group in self.instruments:
            return [k for sid in self.instruments[group]["sections"] for k in self.orchestra.chairs_of(sid)]
        return self.orchestra.chairs_of(group)

    def meaning(self, read, aims=None):
        """Each hand's gesture on the instrument it points at (or its old group); both hands the same gesture: everyone."""
        gestures = {side: g for side, g in read.items() if g != "nothing"}
        if len(gestures) == 2 and gestures["left"] == gestures["right"]:
            return [(gestures["left"], self.groups["both"])]
        meant = []
        for side, g in sorted(gestures.items()):
            group = (aims or {}).get(side) or self.groups[side]
            if (g, group) not in meant:  # two hands on one instrument: one gesture
                meant.append((g, group))
        return meant

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
                                 "height": "the group's gain while open or palm down (the service's, no event)"},
                    "engaged": dict(self.engaged), "instruments": list(self.instruments.values()),
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
        else:
            applied = [o.cue(name, {"dynamics": LEVELS[gesture]}) for name, _, _, _ in events]
            record["applied"] = "; ".join(a for a in applied if a) or "cue kept"
        threads = []
        for name, message, data, to in events:
            entry = {"name": name, "message": message, "data": data, "to": to}
            record["sent"].append(entry)
            if self.dry_run:
                entry["eventId"] = None
                entry["note"] = "dry run: not published"
                continue
            entry["eventId"] = None
            thread = threading.Thread(target=self._publish, args=(entry,), daemon=True)
            thread.start()
            threads.append(thread)
        return threads

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
        return record
