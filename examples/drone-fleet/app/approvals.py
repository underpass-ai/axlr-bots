"""The phone page's side of the base: the calls waiting for a person.

GET /api/approvals lists the tasks waiting for approval with the call each
one stopped on, its mission in a few words and its route on the map;
POST /api/approvals/<task>/approve or /deny relays the decision to the
control plane with the operator token. Both need the approver's code
(X-Approver-Code, from a Secret file), compared in constant time. The page
is for a person on the phone; the panel stays the full tool.
"""

import calendar
import hmac
import logging
import math
import re
import threading
import time
import urllib.parse

log = logging.getLogger("approvals")

TASK_ID = re.compile(r"^task-[0-9a-f]{8,64}$")
CALL_ID = re.compile(r"^[A-Za-z0-9_.:-]{1,128}$")


class Approvals:
    def __init__(self, cp, code_file, route_of):
        self.cp = cp
        self.code_file = code_file
        self.route_of = route_of          # (vehicle, arguments) -> (route [[lat, lon]], distance_m) or (None, 0)
        self.lock = threading.Lock()
        self.cache = (0.0, [])
        self.placed = {}                  # bot -> (when read, the vehicle it runs on)

    @property
    def enabled(self):
        return bool(self.cp and self.cp.enabled and self.code_file)

    def authorised(self, presented):
        if not self.enabled or not presented:
            return False
        try:
            with open(self.code_file) as f:
                code = f.read().strip()
        except OSError:
            return False
        return bool(code) and hmac.compare_digest(presented.encode(), code.encode())

    def vehicle_of(self, bot):
        """The vehicle a bot flies: a pilot's by its name, any other bot's by
        the one node it is placed on (a bot a person added in the panel)."""
        if bot.startswith("pilot-"):
            return bot[len("pilot-"):]
        at, vehicle = self.placed.get(bot, (0.0, ""))
        if time.time() - at < 300:
            return vehicle
        vehicle = ""
        try:
            nodes = ((self.cp.get("/v1/bots/" + urllib.parse.quote(bot, safe="")) or {}).get("placement") or {}).get("nodes") or []
            if len(nodes) == 1 and isinstance(nodes[0], str):
                vehicle = nodes[0]
        except Exception as exc:
            log.warning("bot %s: %s", bot, exc)
        self.placed[bot] = (time.time(), vehicle)
        return vehicle

    def pending(self):
        with self.lock:
            at, items = self.cache
            if time.time() - at < 2:
                return items
        items = []
        for task in self.cp.waiting():
            wait = task.get("approval") or {}
            if wait.get("outcome"):
                continue
            args, call = {}, wait.get("callId", "")
            try:
                for e in reversed(self.cp.executions(task["id"])):
                    pending = (e.get("result") or {}).get("pendingDecision")
                    if pending:
                        args = pending.get("arguments") or {}
                        call = call or pending.get("callId", "")
                        break
            except Exception as exc:
                log.warning("executions of %s: %s", task["id"], exc)
            # AXLR asks for an MCP call as axlr_call_tool {name, why, arguments}: the tool's own arguments are inside.
            why = ""
            if isinstance(args, dict) and isinstance(args.get("arguments"), dict) and isinstance(args.get("name"), str):
                why = str(args.get("why", ""))[:300]
                args = args["arguments"]
            bot = task.get("botId", "")
            vehicle = self.vehicle_of(bot)
            route, distance = self.route_of(vehicle, args) if vehicle and isinstance(args, dict) else (None, 0)
            pattern = args.get("pattern") if isinstance(args, dict) else None
            alts = [w.get("alt_m") for w in args.get("waypoints") or [] if isinstance(w, dict)] if isinstance(args, dict) else []
            items.append({
                "task": task["id"], "bot": bot, "vehicle": vehicle, "objective": task.get("objective", "")[:400],
                "tool": wait.get("tool", ""), "call": call, "since": _epoch(wait.get("since")), "deadline": _epoch(wait.get("deadline")),
                "mission": {
                    "name": str(args.get("name", ""))[:40] if isinstance(args, dict) else "",
                    "pattern": pattern.get("kind") if isinstance(pattern, dict) else None,
                    "target": pattern.get("target") if isinstance(pattern, dict) else None,
                    "waypoints": max(0, len(route) - 1) if route else len(args.get("waypoints") or []) if isinstance(args, dict) else 0,
                    "distance_m": round(distance),
                    "alt_m": (pattern or {}).get("alt_m") if isinstance(pattern, dict) else (max(alts) if alts else None),
                    "photo_every_m": args.get("photo_every_m") if isinstance(args, dict) else None,
                    "deliveries": args.get("deliveries") if isinstance(args, dict) else None,
                },
                "route": route or [],
                "why": why,
                "arguments": args if isinstance(args, dict) else {},
            })
        with self.lock:
            self.cache = (time.time(), items)
        return items

    def decide(self, task_id, decision, call_id):
        if not TASK_ID.match(task_id or "") or (call_id and not CALL_ID.match(call_id)):
            raise ValueError("unknown task or call")
        self.cp.decide(task_id, decision, call_id)
        with self.lock:
            self.cache = (0.0, [])
        log.info("phone: %s %s", decision, task_id)


def _epoch(value):
    if not value:
        return None
    try:
        return float(calendar.timegm(time.strptime(value[:19], "%Y-%m-%dT%H:%M:%S")))
    except ValueError:
        return None


def distance(route):
    from geo import offset_m
    return sum(math.hypot(*offset_m(a[0], a[1], b[0], b[1])) for a, b in zip(route, route[1:]))
