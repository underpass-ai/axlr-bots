"""The conductor's hands: postures from the headset, read by Jev, published on the team's bus.

The page in the headset (static/hands.html, WebXR hand input) measures each
hand's 25 joints: how far each finger bends, where the palm faces, where the
hand is. When a posture has held still for a moment it sends it here (POST
/api/hands), and the service asks Jev, one choice per hand: nothing, open
hand, palm down or fist. Nothing is the default: a hand Jev does not read as a
gesture with at least the threshold's probability gives no signal.

A gesture acts on a group: the left hand on the strings, the right hand on the
winds, both hands showing the same gesture on everyone. It becomes the events
the operator's panel already publishes (orchestra 6b), as the operator:

  open hand   info.cue.<group> {"dynamics": "f"}   come in, or play loud
  palm down   info.cue.<group> {"dynamics": "p"}   come in, or play soft
  fist        stop.<section> to its chairs, one per section of the group;
              stop.all for everyone

The service is the orchestra, so it does what a gesture means at once (holds
the group, sets the level, starts the count-in) and publishes the events in
parallel, without waiting for them to come back on its own bus: the bots
hear them, the service does not apply its own echo (bus.Bus.publish). The
page's answer does not wait for the control plane either: each event's id,
or why it failed, is filled in when the publish answers (GET /api/hands).

An event that would change nothing is not sent, and why is kept: a stop when
nothing of the group plays (a stop.all while idle would still wake 36 bots),
a cue when every chair it names already plays at that level, the same
gesture again within the cooldown. Everything read and sent is kept for the
page.
"""

import threading
import time

GESTURES = {
    "nothing": "nothing: no signal (a hand at rest, relaxed, half-closed, moving, or none of the others)",
    "open": "open hand: fingers straight and apart, the palm facing forward, away from the conductor",
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
        threads = []
        with self.lock:
            for gesture, group in self.meaning(record["read"]):
                record["decision"].append({"gesture": gesture, "group": group})
                threads += self._act(gesture, group, record)
            self._keep(record)
        for thread in threads if self.publish_wait > 0 else ():  # the sound changed already; the ids come when the publish answers
            thread.join(timeout=self.publish_wait)
        return record

    def meaning(self, read):
        """Each hand's gesture on its group; both hands the same gesture: everyone."""
        gestures = {side: g for side, g in read.items() if g != "nothing"}
        if len(gestures) == 2 and gestures["left"] == gestures["right"]:
            return [(gestures["left"], self.groups["both"])]
        return [(g, self.groups[side]) for side, g in sorted(gestures.items())]

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
                    "gestures": {"open": "info.cue.<group> f", "down": "info.cue.<group> p", "fist": "stop.<section> / stop.all"},
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
        named = o.chairs_of(group)
        if gesture == "fist":
            sounding = [k for k in named if chairs[k]["state"] in ("playing", "scheduled")]
            if snap["status"] != "playing" or not sounding:
                record["notSent"].append({"what": f"stop {group}", "why": "nothing of the group plays"})
                return []
            if group == "all":
                events = [("stop.all", "Stop.", None, None)]
            else:
                sections = [sid for sid in o.sections if any(o.chairs[k]["section"] == sid for k in named)]
                events = [(f"stop.{sid}", f"{o.sections[sid]['name']}, stop.", None, [k for k in named if o.chairs[k]["section"] == sid])
                          for sid in sections]
        else:
            level = LEVELS[gesture]
            if snap["status"] == "playing" and all(chairs[k]["dynamics"] == level and not chairs[k]["held"] for k in named) \
                    and any(chairs[k]["state"] in ("playing", "scheduled") for k in named):
                record["notSent"].append({"what": f"{level} {group}", "why": f"the group already plays {level}"})
                return []
            loud = "loud" if level == "f" else "soft"
            events = [(f"info.cue.{group}", f"{'Everyone' if group == 'all' else group.capitalize()}, play {loud}.", {"dynamics": level}, None)]
        self.sent_at[(gesture, group)] = now
        why = f"{gesture} from the conductor's hands"
        if gesture == "fist":
            done = o.stop_all(why) if group == "all" else o.hold(named, why)
            record["applied"] = "cut" if group == "all" and done else (f"held {len(done['held'])} chairs" if done else "nothing plays")
        else:
            record["applied"] = o.cue(f"info.cue.{group}", {"dynamics": LEVELS[gesture]}) or "cue kept"
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
