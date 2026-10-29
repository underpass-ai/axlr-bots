"""The team's bus, as the score service hears it.

The service long-polls the team's timeline (GET /v1/groups/<team>/events
?after=&wait=25s) with the operator token, and reads the operator's own
events in it (group.event.published): stop.all cuts every part at once,
before any bot has stopped; info.tempo with data.bpm sets the clock. This
is the service's own subscription, apart from the bots': the audio does
not wait for 36 urgent tasks. The token is read from its file on each call
and never logged.
"""

import json
import logging
import os
import ssl
import time
import urllib.parse
import urllib.request

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
    if name == "stop.all" or name.startswith("stop."):
        cut = orchestra.stop_all(f"{name} from {payload.get('from', 'operator')}", event_at=at)
        return "cut" if cut else "already stopped"
    if name == "info.tempo" and isinstance(data.get("bpm"), (int, float)):
        orchestra.set_tempo(data["bpm"])
        return "tempo"
    return None


class Bus:
    def __init__(self, url, team, token_file, ca_file, wait="25s"):
        self.url, self.team, self.wait = url.rstrip("/"), team, wait
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
                    what = apply(orchestra, event)
                    if what:
                        log.info("%s %s", (event.get("payload") or {}).get("name"), what)
            except Exception as exc:  # the bus going away must never stop the music
                log.warning("%s; again in 2 s", exc)
                time.sleep(2)
