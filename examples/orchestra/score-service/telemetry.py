"""What the score service measures: GET /metrics for Prometheus, and Jev's questions as JSON lines.

The metrics are a projection of what the service already records for its
page, so a panel and the page never disagree:

  the take          read from Orchestra.snapshot() when Prometheus asks: status,
                    bar, tempo, the chairs playing, held or waiting per section,
                    each section's gain and character
  the orchestra     every fact (Orchestra._fact: a take, a phrase begun, a hold,
                    a cut, a tempo, a level, a character, an event heard) and
                    every cue (Orchestra.cue), counted as they happen
  the chairs        each MCP call (phrases, play, listen, stop): count, latency,
                    refusals; each play: late or not, its lateBars, how far ahead
                    it was asked, the probability the bot reported from Jev
  the hands         every record the hands keep (Hands._keep, or Hands._log
                    where it exists: it also carries the levels left pending):
                    postures read per Jev choice and per side, readings under the
                    threshold, the headset's own gestures, levels pending, kept
                    and reverted by reason, the expression read and applied,
                    what was sent and why not; and from Hands.view(), when
                    Prometheus asks, the provisional and kept level per group
  Jev               each call (judged): count, latency, input tokens, errors,
                    cost estimate; and one JSON line per call (JsonLines)
  the bus           Bus(meter=BUS): publishes per event and their latency, the
                    echo heard back, each delivery to a chair, the timeline's
                    records, errors
  the WebSocket     connections, messages per type and direction
  the render loop   synth.run(meter=RENDER): render time per block, blocks
                    late against the wall clock, clock resets, listeners,
                    blocks a slow listener lost

The modules of the service are not changed for this: watch_orchestra and
watch_hands wrap the few methods every record passes through, on the
instance, so this file is the only place that knows about Prometheus.
Label values come from bounded sets (chairs, sections, gestures, event
names); a reason, an error or a "why" has its numbers replaced by N first.
"""

import collections
import json
import os
import threading
import time

import metrics

REGISTRY = metrics.Registry()
metrics.process_metrics(REGISTRY)

JEV_USD_PER_MILLION = 0.042  # jev-1.13.0, input tokens only: the control plane's costUsd for the Jev chairs, 31 Oct 2026
STATES = ("waiting", "scheduled", "playing", "stopped")
STATUSES = ("idle", "playing", "stopped", "ended")
CHARACTERS = ("plain", "dolce", "cantabile", "brillante", "appassionato")
PROBABILITY = (0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 0.95, 1.0)
BARS = (0, 1, 2, 3, 4, 6, 8)
LEAD = (0, 0.5, 1, 2, 4, 8, 12, 16, 24, 32)
RENDER = (0.0005, 0.001, 0.002, 0.003, 0.005, 0.0075, 0.01, 0.015, 0.02, 0.05, 0.1)
reason = metrics.reason


def section_of(chair):
    return chair.rsplit("-", 1)[0] if "-" in chair else chair


def event_kind(name):
    """info.cue.vn1 → info.cue, stop.ob → stop, info.tempo → info.tempo: one histogram per kind of event."""
    name = str(name or "")
    if name.startswith("info.cue."):
        return "info.cue"
    if name.startswith("stop."):
        return "stop"
    return name or "none"


R = REGISTRY

# --- the chairs' MCP calls ---------------------------------------------------
CHAIR_CALLS = R.counter("orchestra_chair_calls_total", "MCP tool calls by the chairs, per chair, tool and outcome (ok, refused, error).",
                        ("chair", "section", "tool", "outcome"))
CHAIR_SECONDS = R.histogram("orchestra_chair_call_seconds", "How long a chair's tool call took (listen waits up to 20 s), per section and tool.",
                            ("section", "tool"))
PLAYS = R.counter("orchestra_chair_plays_total", "Phrases the chairs asked for (play), late (joined after their first bar) or on time.",
                  ("chair", "section", "late"))
LATE_BARS = R.counter("orchestra_chair_late_bars_total", "Bars missed by the late phrases (the sum of lateBars), per chair.", ("chair", "section"))
LATE_HIST = R.histogram("orchestra_play_late_bars", "lateBars of each play, per section.", ("section",), BARS)
LEAD_HIST = R.histogram("orchestra_play_lead_seconds", "How long before its first bar a phrase was asked for (inSeconds), per section.", ("section",), LEAD)
JUDGE_P = R.histogram("orchestra_chair_judge_probability", "The probability Jev gave the phrase a chair played, as the bot reported it.",
                      ("section",), PROBABILITY)

# --- the orchestra's facts and cues -------------------------------------------
TAKES = R.counter("orchestra_takes_total", "Takes started (a cue or a first play when nothing played).")
TAKES_ENDED = R.counter("orchestra_takes_ended_total", "Takes that reached the movement's end.")
BEGUN = R.counter("orchestra_phrases_begun_total", "Phrases that began sounding, per section.", ("section",))
STOPS = R.counter("orchestra_chair_stops_total", "A chair's own stop, per section.", ("section",))
HOLDS = R.counter("orchestra_holds_total", "Fades out of some chairs (a fist, a stop.<section>).")
HELD = R.counter("orchestra_held_chairs_total", "Chairs faded out, per section.", ("section",))
CUTS = R.counter("orchestra_cuts_total", "stop.all: every part cut at once.")
BUS_TO_SOUND = R.histogram("orchestra_bus_to_sound_seconds", "From an operator's event on the bus (its createdAt) to the sound doing it: cut (stop.all) or hold.",
                           ("what",))
TEMPOS = R.counter("orchestra_tempo_changes_total", "Tempo changes (a swipe, a beat, info.tempo).")
LEVELS_SET = R.counter("orchestra_dynamics_cues_total", "Cues with a level applied while the music plays, per target and level.", ("target", "dynamics"))
CHARACTER_SET = R.counter("orchestra_character_changes_total", "Voices given a character, by the conductor (a group at once) or by a chair's play.",
                          ("character", "by"))
HEARD = R.counter("orchestra_bus_heard_total", "Operator events heard on the team's bus (the service's own echoes too), per name.", ("name",))
CUES = R.counter("orchestra_cues_total", "Cues the orchestra applied (from the bus or the hands), per target, level and what they did.",
                 ("target", "dynamics", "result"))

# --- the hands ----------------------------------------------------------------
POSTURES = R.counter("orchestra_hands_postures_total", "Postures from the headset: read by Jev, Jev failed, or skipped (no hand, too soon).",
                     ("outcome", "simulated"))
JEV_CHOICES = R.counter("orchestra_hands_jev_choices_total", "Jev's choice for each hand, before the threshold.", ("side", "choice"))
GESTURES = R.counter("orchestra_hands_gestures_total", "What each hand was read as after the threshold (nothing included).", ("side", "gesture"))
UNDER = R.counter("orchestra_hands_under_threshold_total", "Readings of a gesture under the threshold, read as nothing.", ("side", "choice"))
GESTURE_P = R.histogram("orchestra_hands_choice_probability", "The probability of Jev's choice for a hand.", ("choice",), PROBABILITY)
DECISIONS = R.counter("orchestra_hands_decisions_total", "What a reading meant: a gesture on a group (posture, native or motion).",
                      ("source", "gesture", "group"))
NOT_SENT = R.counter("orchestra_hands_not_sent_total", "Readings that sent nothing, and why (numbers as N).", ("source", "why"))
HANDS_EVENTS = R.counter("orchestra_hands_events_total", "Events the hands sent on the bus, or would have in a dry run.", ("source", "name", "mode"))
NATIVE = R.counter("orchestra_hands_native_total", "The headset's own gestures (tap, swipes, pinches, double pinch, beat), acted on or refused.",
                   ("gesture", "side", "outcome"))
LEVELS = R.counter("orchestra_hands_levels_total", "A flat hand's provisional level: pending, kept (double pinch, pinch released) or reverted, by reason.",
                   ("kind", "group", "reason"))
MOTIONS = R.counter("orchestra_expression_motions_total", "Motion sums from the headset: read by Jev, Jev failed, or skipped.", ("outcome", "simulated"))
EXPRESSION_CHOICES = R.counter("orchestra_expression_jev_choices_total", "Jev's choice for the expression, before the threshold.", ("choice",))
EXPRESSION_READS = R.counter("orchestra_expression_reads_total", "The expression as read after the threshold (nothing included).", ("character",))
EXPRESSION_CHANGES = R.counter("orchestra_expression_changes_total", "Expressions that changed some voices, per character and group.", ("character", "group"))
EXPRESSION_P = R.histogram("orchestra_expression_probability", "The probability of Jev's choice for the expression.", ("choice",), PROBABILITY)

# --- Jev ----------------------------------------------------------------------
JEV_CALLS = R.counter("orchestra_jev_calls_total", "Calls to Jev by the service (posture: the hands; expression: the motion), ok or error.",
                      ("kind", "outcome"))
JEV_SECONDS = R.histogram("orchestra_jev_seconds", "How long Jev took to answer, errors included.", ("kind",))
JEV_TOKENS = R.counter("orchestra_jev_input_tokens_total", "Input tokens Jev reported.", ("kind",))
JEV_COST = R.counter("orchestra_jev_cost_usd_total", "Estimated cost of Jev's answers: input tokens at the configured price per million.", ("kind",))
JEV_ERRORS = R.counter("orchestra_jev_errors_total", "Jev errors, per kind and reason (numbers as N; the key never appears).", ("kind", "reason"))

# --- the bus ------------------------------------------------------------------
PUBLISHES = R.counter("orchestra_bus_publishes_total", "Events the service published on the team's bus, ok or error.", ("name", "outcome"))
PUBLISH_SECONDS = R.histogram("orchestra_bus_publish_seconds", "From the decision to the control plane's answer: one POST of an event, per kind.", ("kind",))
PUBLISH_ERRORS = R.counter("orchestra_bus_publish_errors_total", "Publishes that failed, per reason.", ("reason",))
ECHOES = R.counter("orchestra_bus_echoes_total", "The service's own events heard back on the bus.", ("kind",))
ECHO_SECONDS = R.histogram("orchestra_bus_echo_seconds", "From a publish's start to its echo on the service's long poll, per kind.", ("kind",))
DELIVERIES = R.counter("orchestra_bus_deliveries_total", "The service's events delivered to a chair (bot.event.heard), per kind and mode.", ("kind", "mode"))
DELIVERY_SECONDS = R.histogram("orchestra_bus_delivery_seconds", "From a publish's start to its delivery to a chair (the control plane's time), per kind and mode.",
                               ("kind", "mode"))
RECORDS = R.counter("orchestra_bus_records_total", "Records the service read on the team's timeline, per type.", ("type",))
APPLIED = R.counter("orchestra_bus_applied_total", "Operators' events the orchestra applied from the bus, per name and what it did.", ("name", "what"))
BUS_ERRORS = R.counter("orchestra_bus_follow_errors_total", "Errors following the team's bus (retried after 2 s).", ("reason",))

# --- the WebSocket, HTTP --------------------------------------------------------
WS_OPEN = R.gauge("orchestra_ws_connections", "Headsets connected on /ws/hands now.")
WS_OPENED = R.counter("orchestra_ws_connections_total", "Connections opened on /ws/hands.")
WS_MESSAGES = R.counter("orchestra_ws_messages_total", "WebSocket messages per direction (up from the headset, down to it) and type.", ("direction", "type"))
HTTP = R.counter("orchestra_http_responses_total", "HTTP responses, per route and status.", ("route", "status"))

# --- the render loop ------------------------------------------------------------
RENDER_SECONDS = R.histogram("orchestra_render_block_seconds", "Time to render one block of audio (the synthesizer and the sequencer).", (), RENDER)
RENDER_LATE = R.counter("orchestra_render_late_blocks_total", "Blocks rendered after their time on the wall clock: a listener's buffer drains.")
RENDER_RESETS = R.counter("orchestra_render_resets_total", "Times the loop fell more than a second behind and took the clock as it was.")
RENDER_LAG = R.gauge("orchestra_render_lag_seconds", "How far the audio rendered is behind the wall clock now (0 when ahead).")
AUDIO_DROPPED = R.counter("orchestra_audio_dropped_blocks_total", "Audio blocks a slow listener lost (its queue full).")


def chair_call(chair, tool, args, seconds, outcome, result=None):
    """One MCP call by a chair, as server.chair_tools saw it."""
    section = section_of(chair)
    CHAIR_CALLS.labels(chair, section, tool, outcome).inc()
    CHAIR_SECONDS.labels(section, tool).observe(seconds)
    if tool != "play" or outcome != "ok" or not isinstance(result, dict):
        return
    late = int(result.get("lateBars") or 0)
    PLAYS.labels(chair, section, "yes" if late else "no").inc()
    if late:
        LATE_BARS.labels(chair, section).inc(late)
    LATE_HIST.labels(section).observe(late)
    LEAD_HIST.labels(section).observe(float(result.get("inSeconds") or 0))
    judge = (args or {}).get("judge")
    p = judge.get("probability") if isinstance(judge, dict) else None
    if isinstance(p, (int, float)) and not isinstance(p, bool) and 0 <= p <= 1:
        JUDGE_P.labels(section).observe(p)


def http(path, status):
    """One HTTP response: the route without its chair or file, the status."""
    path = str(path or "").split("?")[0]
    if path.startswith("/mcp/"):
        route = "/mcp"
    elif path.startswith("/static/"):
        route = "/static"
    elif path in ("/", "/stage", "/hands", "/api/state", "/api/hands", "/api/motion", "/api/score", "/api/take", "/audio.wav", "/ws/hands", "/metrics"):
        route = path
    else:
        route = "other"
    HTTP.labels(route, str(status)).inc()


# --- the orchestra ---------------------------------------------------------------

class _Cached:
    """One snapshot for every family of a scrape."""

    def __init__(self, fn, seconds=1.0):
        self.fn, self.seconds, self.lock, self.at, self.value = fn, seconds, threading.Lock(), None, None

    def __call__(self):
        with self.lock:
            now = time.monotonic()
            if self.at is None or now - self.at > self.seconds:
                self.value, self.at = self.fn(), now
            return self.value


def fact(f):
    """One of the orchestra's facts, as it is kept for the page."""
    what, chair = f.get("what"), f.get("chair")
    if what == "take":
        TAKES.inc()
    elif what == "ended":
        TAKES_ENDED.inc()
    elif what == "playing" and chair:
        BEGUN.labels(section_of(chair)).inc()
    elif what == "stop" and chair:
        STOPS.labels(section_of(chair)).inc()
    elif what == "hold":
        HOLDS.inc()
        for c in f.get("chairs") or ():
            HELD.labels(section_of(c)).inc()
        if isinstance(f.get("afterEventSeconds"), (int, float)):
            BUS_TO_SOUND.labels("hold").observe(max(0.0, f["afterEventSeconds"]))
    elif what == "stop.all":
        CUTS.inc()
        if isinstance(f.get("afterEventSeconds"), (int, float)):
            BUS_TO_SOUND.labels("cut").observe(max(0.0, f["afterEventSeconds"]))
    elif what == "tempo":
        TEMPOS.inc()
    elif what == "dynamics":
        LEVELS_SET.labels(f.get("target") or "none", f.get("dynamics") or "none").inc()
    elif what == "character":
        CHARACTER_SET.labels(f.get("character") or "plain", "chair" if chair else "conductor").inc(1 if chair else int(f.get("chairs") or 1))
    elif what == "bus":
        HEARD.labels(f.get("name") or "none").inc()


def cue_result(result):
    if result is None:
        return "kept"
    text = str(result)
    if text == "count-in":
        return "count-in"
    return "level" if " for " in text else reason(text, 40)


def watch_orchestra(orchestra, rate=None, registry=REGISTRY):
    """Count the orchestra's facts and cues as they happen, and read the take when Prometheus asks."""
    original_fact, original_cue = orchestra._fact, orchestra.cue

    def _fact(what, chair, **data):
        original_fact(what, chair, **data)
        try:
            fact(dict(data, what=what, chair=chair))
        except Exception:  # noqa: BLE001  a metric never stops the music
            pass

    def cue(name, data, at=None):
        result = original_cue(name, data, at)
        target = name[len("info.cue."):] if str(name).startswith("info.cue.") else str(name)
        CUES.labels(target, (data or {}).get("dynamics") or "none", cue_result(result)).inc()
        return result

    orchestra._fact, orchestra.cue = _fact, cue
    rate = rate or orchestra.rate
    snap = _Cached(orchestra.snapshot)

    def now_gains():
        with orchestra.cond:
            return [(c["section"], c["gainNow"]) for c in orchestra.chairs.values()]

    def per_section(fn):
        out = []
        for s in snap()["sections"]:
            out += fn(s)
        return out

    def chairs():
        return per_section(lambda s: [((s["id"], st), sum(1 for c in s["chairs"] if c["state"] == st)) for st in STATES])

    def held():
        return per_section(lambda s: [((s["id"],), sum(1 for c in s["chairs"] if c["held"]))])

    def gain():
        return per_section(lambda s: [((s["id"],), sum(c["gain"] for c in s["chairs"]) / max(1, len(s["chairs"])))])

    def gain_now():
        totals = collections.defaultdict(list)
        for section, g in now_gains():
            totals[section].append(g)
        return [((k,), sum(v) / len(v)) for k, v in totals.items()]

    def character():
        return per_section(lambda s: [((s["id"], ch), sum(1 for c in s["chairs"] if (c["character"] or "plain") == ch)) for ch in CHARACTERS])

    def dynamics():
        return per_section(lambda s: [((s["id"], lv), sum(1 for c in s["chairs"] if c["dynamics"] == lv)) for lv in orchestra.levels])

    def bar():
        s = snap()
        return [((), s["bar"] if s["status"] in ("playing", "stopped", "ended") else 0)]

    registry.computed("orchestra_status", "The take's status: 1 for the one it is in (idle, playing, stopped, ended).", ("status",),
                      lambda: [((st,), 1 if snap()["status"] == st else 0) for st in STATUSES])
    registry.computed("orchestra_take", "The take's number (0 before the first).", (), lambda: [((), snap()["take"])])
    registry.computed("orchestra_bar", "The bar the music is at (during a count-in, the bar it starts at; 0 before a take).", (), bar)
    registry.computed("orchestra_bars", "Bars in the movement.", (), lambda: [((), snap()["bars"])])
    registry.computed("orchestra_bpm", "The tempo, in beats per minute.", (), lambda: [((), snap()["bpm"])])
    registry.computed("orchestra_count_in_bars", "Bars of count-in left (0 when none).", (), lambda: [((), snap()["countIn"])])
    registry.computed("orchestra_chairs", "Chairs per section and state (waiting, scheduled, playing, stopped).", ("section", "state"), chairs)
    registry.computed("orchestra_chairs_held", "Chairs faded out per section, until a cue names them.", ("section",), held)
    registry.computed("orchestra_section_gain", "The gain the conductor set on each section, 0 to 1 (the mean of its chairs).", ("section",), gain)
    registry.computed("orchestra_section_gain_now", "The gain each section sounds at now, on its way to the set one (the mean of its chairs).",
                      ("section",), gain_now)
    registry.computed("orchestra_section_character", "Chairs of each section per character (plain: none).", ("section", "character"), character)
    registry.computed("orchestra_section_dynamics", "Chairs of each section per level.", ("section", "dynamics"), dynamics)
    registry.computed("orchestra_audio_seconds_total", "Seconds of audio rendered since the service started.", (),
                      lambda: [((), orchestra.frames / rate)], kind="counter")
    return orchestra


# --- the hands --------------------------------------------------------------------

def level(gain, via=None):
    """A level record: {"kind": pending|kept|reverted, "group", "why"?}; via names how a level was kept."""
    if not isinstance(gain, dict):
        return
    kind = gain.get("kind") or "none"
    why = {"pending": "flat hand", "kept": via or "double pinch"}.get(kind) or reason(gain.get("why"), 60)
    LEVELS.labels(kind, gain.get("group") or "none", why).inc()


def hands_record(record):
    """One record the hands kept (or logged): a posture, a native gesture, a motion, or a level."""
    if not isinstance(record, dict):
        return
    simulated = "yes" if record.get("simulated") else "no"
    if "native" in record:
        source = "native"
        acted = bool(record.get("decision") or record.get("applied")) or not record.get("notSent")
        NATIVE.labels(str(record.get("native")), record.get("side") or "none", "acted" if acted else "refused").inc()
        if isinstance(record.get("gain"), dict):
            level(record["gain"], via="pinch released")
    elif "motion" in record:
        source = "motion"
        jev = record.get("jev") or {}
        answer = (jev.get("answers") or {}).get("expression")
        MOTIONS.labels("read" if answer else "jev error" if jev.get("error") else "skipped", simulated).inc()
        if answer:
            choice = answer.get("choice") or "nothing"
            EXPRESSION_CHOICES.labels(choice).inc()
            p = (answer.get("probabilities") or {}).get(choice)
            if isinstance(p, (int, float)):
                EXPRESSION_P.labels(choice).observe(p)
            EXPRESSION_READS.labels((record.get("read") or {}).get("expression") or "nothing").inc()
        if record.get("decision") and not str(record.get("applied") or "").endswith(" already"):
            for d in record["decision"]:
                EXPRESSION_CHANGES.labels(d.get("gesture") or "none", d.get("group") or "none").inc()
    elif "hands" in record:
        source = "posture"
        jev = record.get("jev") or {}
        answers = jev.get("answers") or {}
        POSTURES.labels("read" if answers else "jev error" if jev.get("error") else "skipped", simulated).inc()
        for side, a in answers.items():
            choice = a.get("choice") or "nothing"
            read = (record.get("read") or {}).get(side) or "nothing"
            JEV_CHOICES.labels(side, choice).inc()
            GESTURES.labels(side, read).inc()
            if choice != "nothing" and read == "nothing":
                UNDER.labels(side, choice).inc()
            p = (a.get("probabilities") or {}).get(choice)
            if isinstance(p, (int, float)):
                GESTURE_P.labels(choice).observe(p)
    elif isinstance(record.get("gain"), dict):
        level(record["gain"])
        return
    else:
        return
    for d in record.get("decision") or ():
        DECISIONS.labels(source, d.get("gesture") or "none", d.get("group") or "none").inc()
    for n in record.get("notSent") or ():
        NOT_SENT.labels(source, reason(n.get("why"), 60)).inc()
    for s in record.get("sent") or ():
        HANDS_EVENTS.labels(source, s.get("name") or "none", "dry run" if record.get("dryRun") else "published").inc()


def watch_hands(hands, registry=REGISTRY):
    """Count every record the hands keep: through _log where the hands have it (every kept record and the levels
    left pending pass there), else _keep. And read the hands' levels when Prometheus asks."""
    name = "_log" if callable(getattr(hands, "_log", None)) else "_keep"
    original = getattr(hands, name)

    def through(record):
        out = original(record)
        try:
            hands_record(record)
        except Exception:  # noqa: BLE001  a metric never stops the hands
            pass
        return out

    setattr(hands, name, through)
    view = _Cached(hands.view)

    def pending():
        return [((side, p.get("group") or "none"), p.get("gain") or 0) for side, p in (view().get("pending") or {}).items() if isinstance(p, dict)]

    def kept():
        return [((group,), gain) for group, gain in (view().get("confirmed") or {}).items() if isinstance(gain, (int, float))]

    def engaged():
        out = []
        for side, e in (view().get("engaged") or {}).items():
            if isinstance(e, dict):
                out.append(((side, e.get("gesture") or "none", e.get("group") or "none"), 1))
            elif isinstance(e, (tuple, list)) and len(e) == 2:
                out.append(((side, e[0], e[1]), 1))
        return out

    def expression():
        c = view().get("character")
        return [((c.get("character") or "none", c.get("group") or "none"), 1)] if isinstance(c, dict) else []

    registry.computed("orchestra_hands_level_pending", "A flat hand's provisional level now, per hand and group (heard, not kept yet).", ("side", "group"), pending)
    registry.computed("orchestra_hands_level_kept", "Each group's last level kept (a double pinch, a pinch released).", ("group",), kept)
    registry.computed("orchestra_hands_engaged", "A flat hand conducting a group's level now: 1 per hand, gesture and group.", ("side", "gesture", "group"), engaged)
    registry.computed("orchestra_expression_current", "The last expression read and applied: 1 for its character and group.", ("character", "group"), expression)
    registry.computed("orchestra_hands_dry_run", "1 when the hands publish nothing (--hands-dry-run).", (), lambda: [((), 1 if view().get("dryRun") else 0)])
    return hands


# --- Jev ------------------------------------------------------------------------------

def jev_kind(questions):
    keys = set(questions or ())
    if keys and keys <= {"left", "right"}:
        return "posture"
    if "expression" in keys:
        return "expression"
    return "other"


def judged(choose, log=None, usd_per_million=JEV_USD_PER_MILLION, clock=time.time):
    """choose(state, questions) measured: each call counted, timed, its tokens and cost added, and written to log."""

    def call(state, questions):
        kind, started, at = jev_kind(questions), time.monotonic(), clock()
        entry = {"at": round(at, 3), "kind": kind, "state": state,
                 "questions": {k: {"instructions": q[0], "options": list(q[1])} for k, q in (questions or {}).items()}}
        try:
            answer = choose(state, questions)
        except Exception as exc:
            seconds = time.monotonic() - started
            JEV_CALLS.labels(kind, "error").inc()
            JEV_SECONDS.labels(kind).observe(seconds)
            JEV_ERRORS.labels(kind, reason(exc, 60)).inc()
            if log is not None:
                log.write(dict(entry, ms=round(seconds * 1000), error=str(exc)))
            raise
        seconds = time.monotonic() - started
        tokens = answer.get("inputTokens") if isinstance(answer, dict) else None
        tokens = tokens if isinstance(tokens, (int, float)) and not isinstance(tokens, bool) else 0
        cost = tokens * usd_per_million / 1e6
        JEV_CALLS.labels(kind, "ok").inc()
        JEV_SECONDS.labels(kind).observe(seconds)
        JEV_TOKENS.labels(kind).inc(tokens)
        JEV_COST.labels(kind).inc(cost)
        if log is not None:
            log.write(dict(entry, model=answer.get("model"), ms=answer.get("ms"), inputTokens=tokens, costUsd=round(cost, 8),
                           answers=answer.get("answers")))
        return answer

    call.measured = choose
    return call


class JsonLines:
    """One JSON object per line, appended; a path that is a directory gets one file per run: jev-<UTC time>.jsonl."""

    def __init__(self, path, prefix="jev", clock=time.time):
        path = os.path.expanduser(path)
        if path.endswith(os.sep) or os.path.isdir(path):
            os.makedirs(path, exist_ok=True)
            path = os.path.join(path, f"{prefix}-{time.strftime('%Y%m%dT%H%M%SZ', time.gmtime(clock()))}-{os.getpid()}.jsonl")
        self.path, self.lock, self.errors = path, threading.Lock(), 0

    def write(self, obj):
        try:
            line = json.dumps(obj, ensure_ascii=False, default=str) + "\n"
            with self.lock, open(self.path, "a", encoding="utf-8") as f:
                f.write(line)
        except (OSError, TypeError, ValueError):
            self.errors += 1  # a log failing never fails a reading


# --- the bus, the render loop ---------------------------------------------------------------

class BusMeter:
    """What bus.Bus tells about its publishes, echoes, deliveries and records."""

    def published(self, name, seconds, error=None):
        PUBLISHES.labels(name, "error" if error else "ok").inc()
        PUBLISH_SECONDS.labels(event_kind(name)).observe(seconds)
        if error:
            PUBLISH_ERRORS.labels(reason(error, 60)).inc()

    def echoed(self, name, seconds):
        ECHOES.labels(event_kind(name)).inc()
        ECHO_SECONDS.labels(event_kind(name)).observe(max(0.0, seconds))

    def delivered(self, name, mode, seconds):
        DELIVERIES.labels(event_kind(name), mode or "none").inc()
        DELIVERY_SECONDS.labels(event_kind(name), mode or "none").observe(max(0.0, seconds))

    def seen(self, record_type):
        RECORDS.labels(record_type or "none").inc()

    def applied(self, name, what):
        APPLIED.labels(name or "none", reason(what, 40) if what else "nothing").inc()

    def failed(self, error):
        BUS_ERRORS.labels(reason(error, 60)).inc()


class RenderMeter:
    """What synth.run and synth.Stream tell about each block."""

    def rendered(self, seconds):
        RENDER_SECONDS.observe(seconds)

    def paced(self, lag, reset):
        RENDER_LAG.set(max(0.0, lag))
        if lag > 0:
            RENDER_LATE.inc()
        if reset:
            RENDER_RESETS.inc()

    def dropped(self, blocks):
        AUDIO_DROPPED.inc(blocks)


BUS = BusMeter()
RENDER_METER = RenderMeter()


def watch_stream(stream, registry=REGISTRY):
    registry.computed("orchestra_audio_listeners", "Listeners of /audio.wav now.", (), lambda: [((), stream.count())])
    return stream


def ws_message(direction, kind):
    WS_MESSAGES.labels(direction, kind).inc()
