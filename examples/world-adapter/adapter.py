#!/usr/bin/env python3
"""A world adapter: the template (docs/architecture/input-adapters.md).

A bot reads text and calls tools; a world is positions, pixels, audio, a
hand. This process sits beside a world service and turns its signal into
the things a bot already understands:

  observe(bot_id)   one reading as text, for a tool the world serves (an
                    MCP tool on loopback, or a file in the bot's read-only
                    workspace). Paced: at most rate.perMinute readings of
                    the world a minute; past that it answers from the last
                    reading and says how old it is
  watch()           the world's changes as events for the team's bus:
                    Event(name, message, data, to), published with
                    POST /v1/groups/<team>/events as the operator. An
                    info.* lands as a note, a warn.* as a line in a running
                    task, a danger.* or stop.* as an urgent task for every
                    member of the team

The world here is a fake in memory (World): a few things on a unit square
with a fence and a mark, moved by a seeded generator, so the adapter runs
and its tests pass with nothing installed and no network. A real adapter
replaces World with a client of the world service and keeps the rest.
One bot per thing: the bot's id is the thing's id.

Run it:  python3 adapter.py [config.json] [--ticks N]   readings and events on stdout; publishes only with a url
Test it: python3 -m unittest -v test_adapter
"""

import hashlib
import json
import math
import os
import random
import ssl
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from collections import namedtuple

HERE = os.path.dirname(os.path.abspath(__file__))
LABEL = "Simulated world. Each creature is one LLM bot; it sees only what this adapter reports, as text."

# One event for the bus: the route's body, less the key, which is derived.
Event = namedtuple("Event", "name message data to", defaults=(None,))

# observe as an MCP tool, for mcp_http.Tools.add(TOOL, lambda a: {"text": adapter.observe(a["bot"])}).
TOOL = {
    "name": "observe",
    "description": "What the world shows at your place now, as one line of text: where you are, the mark, "
                   "the nearest creature, the fence, and how old the reading is.",
    "inputSchema": {"type": "object", "properties": {"bot": {"type": "string", "description": "your bot id"}},
                    "required": ["bot"], "additionalProperties": False},
}


class World:
    """The fake world: things on a unit square, a fence, a mark; deterministic from its seed.

    x and y are fractions of the frame from its top-left corner; y grows south.
    The fence is the square from 1 - fence to fence on each axis.
    """

    def __init__(self, things=12, seed=7, fence=0.9, mark=(0.5, 0.5), step=0.03, drift=True, url=""):
        self.rng = random.Random(seed)
        self.fence = fence
        self.mark = tuple(mark)
        self.step = step          # how far a thing moves in a tick: the unit of distance in the text
        self.drift = drift        # False: a still world, moved only by a hand (the tests)
        self.things = {f"thing-{i}": [round(self.rng.uniform(0.2, 0.8), 3), round(self.rng.uniform(0.2, 0.8), 3)]
                       for i in range(things)}
        self.ticks = 0
        self.reads = 0

    def read(self):
        """One frame, as the world service would answer it: every thing's position. Each read is counted."""
        self.reads += 1
        return {"tick": self.ticks, "things": {k: (v[0], v[1]) for k, v in self.things.items()}}

    def tick(self):
        """One step of the world: every thing drifts by at most step on each axis."""
        self.ticks += 1
        if not self.drift:
            return
        for pos in self.things.values():
            pos[0] = round(min(1.0, max(0.0, pos[0] + self.rng.uniform(-self.step, self.step))), 3)
            pos[1] = round(min(1.0, max(0.0, pos[1] + self.rng.uniform(-self.step, self.step))), 3)

    def place(self, thing, x, y):
        """A hand on the world: put a thing somewhere (the tests, a demo)."""
        self.things[thing] = [x, y]

    def outside(self, x, y):
        low = round(1 - self.fence, 3)
        return not (low <= x <= self.fence and low <= y <= self.fence)

    def at_mark(self, x, y):
        return math.hypot(x - self.mark[0], y - self.mark[1]) <= self.step


class Rate:
    """At most per_minute expensive readings a minute, on an injectable clock."""

    def __init__(self, per_minute, now=time.monotonic):
        self.per_minute = max(1, int(per_minute))
        self.now = now
        self.times = []

    def allow(self):
        t = self.now()
        self.times = [x for x in self.times if t - x < 60]
        if len(self.times) >= self.per_minute:
            return False
        self.times.append(t)
        return True


def direction(dx, dy):
    """north-east, south, west…: y grows south."""
    ns = "north" if dy < 0 else "south" if dy > 0 else ""
    ew = "east" if dx > 0 else "west" if dx < 0 else ""
    return "-".join(p for p in (ns, ew) if p) or "here"


class Adapter:
    def __init__(self, world, team, per_minute=6, now=time.monotonic):
        self.world = world
        self.team = team
        self.rate = Rate(per_minute, now)
        self.now = now
        self.frame = None        # the last reading of the world
        self.frame_at = None     # when it was read

    def _fresh(self):
        """A new frame, when the rate allows; else the last one."""
        if self.rate.allow() or self.frame is None:
            self.frame = self.world.read()
            self.frame_at = self.now()
        return self.frame

    def _steps(self, x, y, tx, ty):
        dx, dy = tx - x, ty - y
        return max(1, round(math.hypot(dx, dy) / self.world.step)), direction(dx, dy)

    def observe(self, bot_id):
        """One reading for one bot, as one line: where it is, the fence, the mark, the nearest thing, the reading's age.

        Exact (no model), and it says so. An unknown bot is a LookupError: serve it as a tool error.
        """
        frame = self._fresh()
        thing = frame["things"].get(bot_id)
        if thing is None:
            raise LookupError(f"{bot_id} is not in this world")
        x, y = thing
        fence = "outside" if self.world.outside(x, y) else "inside"
        steps, heading = self._steps(x, y, *self.world.mark)
        others = sorted((math.hypot(ox - x, oy - y), k, ox, oy) for k, (ox, oy) in frame["things"].items() if k != bot_id)
        if others:
            _, name, ox, oy = others[0]
            osteps, oheading = self._steps(x, y, ox, oy)
            nearest = f"{name} at {ox:.2f}, {oy:.2f}, {osteps} steps {oheading}"
        else:
            nearest = "nobody"
        age = max(0.0, self.now() - self.frame_at)
        return (f"{bot_id} at {x:.2f}, {y:.2f}, {fence} the fence; the mark at {self.world.mark[0]:.2f}, {self.world.mark[1]:.2f} "
                f"is {steps} steps {heading}; nearest: {nearest}. Reading {age:.0f} s old; exact, simulated.")

    def watch(self, ticks=None, sleep=None):
        """The world's changes as events, one tick at a time, forever when ticks is None.

        A thing that crosses the fence: warn.fence, addressed to its bot (heard as a line in its
        running task, a task when idle, with no subscription needed). A thing that reaches the
        mark: info.reached, for whoever subscribes (a note by default). Each tick refreshes the
        frame observe answers from, free of the rate: this loop is the adapter's own pace.
        """
        previous = self.frame if self.frame is not None else self.world.read()
        n = 0
        while ticks is None or n < ticks:
            if sleep is not None:
                sleep()
            self.world.tick()
            frame = self.world.read()
            self.frame, self.frame_at = frame, self.now()
            for thing, (x, y) in frame["things"].items():
                px, py = previous["things"].get(thing, (x, y))
                if self.world.outside(x, y) and not self.world.outside(px, py):
                    yield Event("warn.fence", f"{thing} crossed the fence at {x:.2f}, {y:.2f}; come back inside",
                                {"thing": thing, "x": x, "y": y, "tick": frame["tick"]}, [thing])
                if self.world.at_mark(x, y) and not self.world.at_mark(px, py):
                    yield Event("info.reached", f"{thing} reached the mark at {x:.2f}, {y:.2f}",
                                {"thing": thing, "x": x, "y": y, "tick": frame["tick"]})
            previous = frame
            n += 1


def body_of(event):
    """The route's body: name, message, data, and to when the event is addressed."""
    body = {"name": event.name, "message": event.message, "data": event.data}
    if event.to:
        body["to"] = list(event.to)
    return body


def key_of(body):
    """The Idempotency-Key: the same change again is the same event, and the control plane makes nothing twice."""
    return "evt-" + hashlib.sha256(json.dumps(body, sort_keys=True).encode()).hexdigest()[:32]


class Publisher:
    """Puts events on the team's bus: POST /v1/groups/<team>/events with the operator token.

    The token is read from its file on each call and never logged; the fleet CA verifies the
    control plane. Without a url it prints what it would send and publishes nothing.
    """

    def __init__(self, team, url="", token_file="", ca_file="", post=None, out=sys.stdout):
        self.team = team
        self.url = url.rstrip("/")
        self.token_file = token_file
        self.ca_file = ca_file
        self.post = post or self._post
        self.out = out
        self._ctx = None

    def publish(self, event):
        body = body_of(event)
        key = key_of(body)
        if not self.url:
            print(f"would publish {key} {json.dumps(body)}", file=self.out)
            return {"id": key, "dryRun": True}
        with open(self.token_file) as f:
            token = f.read().strip()
        headers = {"Content-Type": "application/json", "Authorization": "Bearer " + token, "Idempotency-Key": key}
        return self.post(f"{self.url}/v1/groups/{urllib.parse.quote(self.team)}/events", body, headers)

    def _post(self, url, body, headers, timeout=10):
        if self._ctx is None:
            self._ctx = ssl.create_default_context(cafile=self.ca_file or None)
        request = urllib.request.Request(url, data=json.dumps(body).encode(), method="POST", headers=headers)
        try:
            with urllib.request.urlopen(request, timeout=timeout, context=self._ctx) as r:
                return json.load(r)
        except urllib.error.HTTPError as exc:
            try:
                message = json.loads(exc.read()).get("error", exc.reason)
            except Exception:
                message = exc.reason
            raise RuntimeError(f"publish {body['name']}: {exc.code} {message}") from None


def load(path):
    with open(path) as f:
        return json.load(f)


def build(config, now=time.monotonic):
    """The world, the adapter and the publisher a config names."""
    world_config = dict(config.get("world") or {})
    if world_config.get("url"):
        raise NotImplementedError("world.url names a world service: replace World with its client")
    world = World(**world_config)
    adapter = Adapter(world, config["team"], (config.get("rate") or {}).get("perMinute", 6), now)
    cp = config.get("controlPlane") or {}
    publisher = Publisher(config["team"], cp.get("url", ""), cp.get("tokenFile", ""), cp.get("caFile", ""))
    return world, adapter, publisher


def main(argv):
    path, ticks = os.path.join(HERE, "config.json"), None
    args = list(argv[1:])
    while args:
        arg = args.pop(0)
        if arg == "--ticks":
            ticks = int(args.pop(0))
        else:
            path = arg
    config = load(path)
    world, adapter, publisher = build(config)
    period = float(config.get("period", 1.0))
    print(config.get("label", LABEL))
    print(adapter.observe("thing-0"))
    sleep = (lambda: time.sleep(period)) if ticks is None else None
    for event in adapter.watch(ticks, sleep):
        publisher.publish(event)
    if ticks is not None:
        print(adapter.observe("thing-0"))
        print(f"{world.ticks} ticks, {world.reads} readings of the world")


if __name__ == "__main__":
    main(sys.argv)
