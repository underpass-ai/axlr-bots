"""The team's bus, as the score service hears it.

The service long-polls the team's timeline (GET /v1/groups/<team>/events
?after=&wait=25s) with the operator token, and reads the operator's own
events in it (group.event.published): stop.all cuts every part at once,
before any bot has stopped; another stop.* stops only the chairs it is
addressed to (to), or the section or family its name ends with (stop.vn1,
stop.strings), and the others play on; a stop.* that names neither is read as
stop.all; info.tempo with data.bpm sets the clock; a cue (info.cue.*) when
nothing plays starts a take with a count-in, from bar 1 or data.bar, so the
chairs cued choose their first phrase before it, and while the music plays
releases the chairs it names and sets their data.dynamics at once. This
is the service's own subscription, apart from the bots': the audio does
not wait for 36 urgent tasks. The token is read from its file on each call
and never logged.

With a meter (telemetry.BusMeter), the bus also says how long each publish
took, when the service's own event came back on its long poll (the echo,
from the publish's start), and when the control plane delivered it to each
chair (bot.event.heard, its createdAt against the publish's start, both on
the host's clock): a delivery may be recorded before the published event
itself, so deliveries of an event not yet known wait, a bounded few, for its
id.
"""

import collections
import json
import logging
import os
import ssl
import threading
import time
import urllib.parse
import urllib.request
import uuid

log = logging.getLogger("bus")


def parse_time(text):
    text = text.replace("Z", "+00:00")
    head, _, tail = text.partition(".")
    if tail:
        digits = tail[:len(tail) - len(tail.lstrip("0123456789"))]
        text = f"{head}.{digits[:6]}{tail[len(digits):]}"
    import datetime

    return datetime.datetime.fromisoformat(text).timestamp()


def apply(orchestra, event, clock=time.time):
    """One timeline record → what the orchestra does with it; returns a word for the log."""
    if event.get("type") != "group.event.published":
        return None
    payload = event.get("payload") or {}
    name, data = payload.get("name", ""), payload.get("data") or {}
    at = parse_time(event["createdAt"]) if event.get("createdAt") else None
    orchestra.heard(name, payload.get("message", ""), data, at)
    if name.startswith("stop.") and name != "stop.all":
        chairs = [k for k in payload.get("to") or [] if k in orchestra.chairs] or orchestra.chairs_of(name[len("stop."):])
        if chairs and len(chairs) < len(orchestra.chairs):
            held = orchestra.hold(chairs, f"{name} from {payload.get('from', 'operator')}", event_at=at)
            return f"held {len(held['held'])} chairs" if held else "nothing plays"
    if name.startswith("stop."):
        cut = orchestra.stop_all(f"{name} from {payload.get('from', 'operator')}", event_at=at)
        return "cut" if cut else "already stopped"
    if name.startswith("info.cue."):
        return orchestra.cue(name, data, at)
    if name == "info.tempo" and isinstance(data.get("bpm"), (int, float)):
        orchestra.set_tempo(data["bpm"])
        return "tempo"
    return None


def own_mark(name, message, to):
    return (name, message, tuple(sorted(to or ())))


class Bus:
    KNOWN = 512  # the service's events whose deliveries are still measured, and the deliveries waiting for an id

    def __init__(self, url, team, token_file, ca_file, wait="25s", meter=None):
        self.url, self.team, self.wait, self.meter = url.rstrip("/"), team, wait, meter
        self.own, self.own_lock = [], threading.Lock()  # events this service published, whose echo it does not apply
        self.started = {}  # each own mark: when its publishes started (wall clock), oldest first
        self.sent = collections.OrderedDict()  # an event id this service published → (name, when its publish started)
        self.early = collections.OrderedDict()  # an event id not known yet → [(mode, createdAt)] of its deliveries
        self.token_file, self.ca_file = os.path.expanduser(token_file), os.path.expanduser(ca_file)
        self.context = ssl.create_default_context(cafile=self.ca_file)

    def events(self, after=None, wait=None):
        query = {"limit": "200"}
        if after:
            query["after"] = after
        if wait:
            query["wait"] = wait
        request = urllib.request.Request(f"{self.url}/v1/groups/{urllib.parse.quote(self.team)}/events?{urllib.parse.urlencode(query)}")
        with open(self.token_file) as f:
            request.add_header("Authorization", "Bearer " + f.read().strip())
        with urllib.request.urlopen(request, context=self.context, timeout=40) as response:
            return json.load(response)

    def publish(self, name, message, data=None, to=None):
        """Publish one event on the team as the operator, as the panel's shortcuts do; returns its id.

        The service did what the event means before publishing it: its echo on
        the bus is heard (the page shows it) but not applied again."""
        mark = own_mark(name, message, to)
        started, began = time.time(), time.monotonic()
        with self.own_lock:
            self.own.append(mark)
            self.started.setdefault(mark, []).append(started)
        body = {"name": name, "message": message}
        if data:
            body["data"] = data
        if to:
            body["to"] = list(to)
        request = urllib.request.Request(f"{self.url}/v1/groups/{urllib.parse.quote(self.team)}/events",
                                         data=json.dumps(body).encode(), method="POST")
        with open(self.token_file) as f:
            request.add_header("Authorization", "Bearer " + f.read().strip())
        request.add_header("Content-Type", "application/json")
        request.add_header("Idempotency-Key", f"hands-{uuid.uuid4().hex[:16]}")
        try:
            with urllib.request.urlopen(request, context=self.context, timeout=10) as response:
                event_id = json.load(response).get("eventId")
        except Exception as exc:
            self._take_own(mark)
            if self.meter:
                self.meter.published(name, time.monotonic() - began, exc)
            raise
        if self.meter:
            self.meter.published(name, time.monotonic() - began)
            self._known(event_id, name, started)
        return event_id

    def _take_own(self, mark):
        """Whether mark is one of this service's publishes not heard back yet (then forgotten), and when it started."""
        with self.own_lock:
            if mark not in self.own:
                return False, None
            self.own.remove(mark)
            starts = getattr(self, "started", {}).get(mark) or []
            started = starts.pop(0) if starts else None
            if not starts:
                getattr(self, "started", {}).pop(mark, None)
            return True, started

    def claim(self, event):
        """(own, started): whether this published event is the service's own echo, and when its publish started."""
        payload = event.get("payload") or {}
        if event.get("type") != "group.event.published":
            return False, None
        return self._take_own(own_mark(payload.get("name", ""), payload.get("message", ""), payload.get("to")))

    def is_own(self, event):
        return self.claim(event)[0]

    def _known(self, event_id, name, started):
        """An id of the service's own event, and when its publish started: its deliveries are measured from then."""
        if not event_id or started is None:
            return
        with self.own_lock:
            self.sent[event_id] = (name, started)
            while len(self.sent) > self.KNOWN:
                self.sent.popitem(last=False)
            waiting = self.early.pop(event_id, [])
        for mode, created in waiting:
            self.meter.delivered(name, mode, created - started)

    def observe(self, event):
        """One record of the timeline, for the meter: its type, and a delivery of the service's own events."""
        self.meter.seen(event.get("type"))
        if event.get("type") != "bot.event.heard" or not event.get("createdAt"):
            return
        payload = event.get("payload") or {}
        event_id, mode, created = payload.get("eventId"), payload.get("mode"), parse_time(event["createdAt"])
        if not event_id:
            return
        with self.own_lock:
            known = self.sent.get(event_id)
            if known is None:
                self.early.setdefault(event_id, []).append((mode, created))
                while len(self.early) > self.KNOWN:
                    self.early.popitem(last=False)
                return
        self.meter.delivered(known[0], mode, created - known[1])

    def follow(self, orchestra, stop=None):
        """Hear the team from now on; a team that does not exist yet is retried."""
        after = None
        while not (stop and stop.is_set()):
            try:
                if after is None:
                    newest = self.events()
                    after = newest[-1]["eventId"] if newest else ""
                    log.info("following team %s from %s", self.team, after or "the start")
                    continue
                for event in self.events(after=after or None, wait=self.wait):
                    after = event["eventId"]
                    if self.meter:
                        self.observe(event)
                    own, started = self.claim(event)
                    if own:
                        payload = event.get("payload") or {}
                        orchestra.heard(payload.get("name", ""), payload.get("message", ""), payload.get("data") or {},
                                        parse_time(event["createdAt"]) if event.get("createdAt") else None)
                        log.info("%s heard back (the service did it already)", payload.get("name"))
                        if self.meter and started is not None:
                            self.meter.echoed(payload.get("name", ""), time.time() - started)
                            self._known(payload.get("eventId"), payload.get("name", ""), started)
                        continue
                    what = apply(orchestra, event)
                    if self.meter and event.get("type") == "group.event.published":
                        self.meter.applied((event.get("payload") or {}).get("name"), what)
                    if what:
                        log.info("%s %s", (event.get("payload") or {}).get("name"), what)
            except Exception as exc:  # the bus going away must never stop the music
                if self.meter:
                    self.meter.failed(exc)
                log.warning("%s; again in 2 s", exc)
                time.sleep(2)
